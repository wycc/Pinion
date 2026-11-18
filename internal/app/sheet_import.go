package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ka1hung/pinion/internal/importer/bookmarks"
	"github.com/ka1hung/pinion/internal/importer/sheets"
	"github.com/ka1hung/pinion/internal/store"
)

// aiKeySecret returns the credential-store account holding a provider's API key.
func aiKeySecret(provider string) (string, error) {
	switch provider {
	case "", sheets.ProviderAnthropic:
		return "ai:anthropic-api-key", nil
	case sheets.ProviderOpenRouter:
		return "ai:openrouter-api-key", nil
	}
	return "", fmt.Errorf("unknown AI provider %q", provider)
}

// SheetRequest describes a Google Sheets import.
type SheetRequest struct {
	URL         string `json:"url"`
	DefaultHost string `json:"defaultHost"`
	DefaultUser string `json:"defaultUser"`
	Folder      string `json:"folder"`
	UseAI       bool   `json:"useAI"`
}

// SheetPreview is what a sheet would import.
type SheetPreview struct {
	Result *bookmarks.Result `json:"result"`
	Method string            `json:"method"` // "ai" or "rules"
	Diff   sheets.Diff       `json:"diff"`   // rows in the sheet / analysed this time / gone
}

// ImportCount reports the outcome of an import.
type ImportCount struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
}

// RefreshResult reports what "refresh list" did.
type RefreshResult struct {
	Diff     sheets.Diff `json:"diff"`
	Method   string      `json:"method"`
	Sessions int         `json:"sessions"` // sessions found in the sheet
	Added    int         `json:"added"`
	Updated  int         `json:"updated"`
	Skipped  int         `json:"skipped"`
	Missing  int         `json:"missing"` // sessions in the target folder no longer in the sheet (kept)
}

// sheetMu serialises use of the row cache file.
var sheetMu sync.Mutex

func (s *ImportService) cachePath() string { return filepath.Join(s.c.Store.Dir(), "sheet_cache.json") }

func (s *ImportService) loadCache(url string) *sheets.Cache {
	b, err := os.ReadFile(s.cachePath())
	if err != nil {
		return nil
	}
	var c sheets.Cache
	if json.Unmarshal(b, &c) != nil || c.URL != url {
		return nil // another sheet: start over
	}
	return &c
}

func (s *ImportService) saveCache(c *sheets.Cache) {
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	tmp := s.cachePath() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.cachePath())
	}
}

// analyzeSheet downloads the sheet, compares it with the cached previous
// version and analyses only rows that changed (rules or AI), then builds the
// sessions from all rows.
func (s *ImportService) analyzeSheet(ctx context.Context, req SheetRequest) (*SheetPreview, error) {
	url := strings.TrimSpace(req.URL)
	rows, err := sheets.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	method, label := sheets.MethodRules, "rules"
	var ai sheets.AIFunc
	if req.UseAI {
		cur := s.c.Store.Settings()
		name, err := aiKeySecret(cur.AIProvider)
		if err != nil {
			return nil, err
		}
		key, kerr := s.c.Secrets.Get(name)
		if kerr != nil || key == "" {
			return nil, errors.New("no API key set for the selected AI service (Settings → AI)")
		}
		label = "ai"
		if cur.AIProvider == sheets.ProviderOpenRouter {
			method = "ai:openrouter:" + cur.OpenRouterModel
			ai = func(ctx context.Context, text string) ([]sheets.Entry, error) {
				return sheets.ParseOpenRouter(ctx, key, cur.OpenRouterModel, text)
			}
		} else {
			method = "ai:anthropic:" + cur.AIModel
			ai = func(ctx context.Context, text string) ([]sheets.Entry, error) {
				return sheets.ParseAI(ctx, key, cur.AIModel, text)
			}
		}
	}

	sheetMu.Lock()
	defer sheetMu.Unlock()
	entries, diff, next, warnings, err := sheets.Analyze(ctx, rows, method, s.loadCache(url), ai)
	if err != nil {
		return nil, err
	}
	next.URL = url
	s.saveCache(next)

	res := sheets.Build(entries, sheets.Options{DefaultHost: req.DefaultHost, DefaultUser: req.DefaultUser})
	res.Warnings = append(warnings, res.Warnings...)
	return &SheetPreview{Result: res, Method: label, Diff: diff}, nil
}

// PreviewSheet downloads the sheet and parses it without saving sessions. The
// request is remembered in settings so "refresh list" can repeat it.
func (s *ImportService) PreviewSheet(req SheetRequest) (*SheetPreview, error) {
	p, err := s.analyzeSheet(context.Background(), req)
	if err != nil {
		return nil, err
	}
	st := s.c.Store.Settings()
	st.SheetURL = strings.TrimSpace(req.URL)
	st.SheetDefaultHost = strings.TrimSpace(req.DefaultHost)
	st.SheetDefaultUser = strings.TrimSpace(req.DefaultUser)
	st.SheetFolder = req.Folder
	st.SheetUseAI = req.UseAI
	_ = s.c.Store.SaveSettings(st)
	return p, nil
}

// RefreshSheet re-reads the last imported sheet with the saved options,
// re-analyses only the rows that changed, and updates the sessions: changed
// host/port/user are updated in place and new ones added. Sessions whose rows
// were removed from the sheet are counted but not deleted.
func (s *ImportService) RefreshSheet() (*RefreshResult, error) {
	st := s.c.Store.Settings()
	if strings.TrimSpace(st.SheetURL) == "" {
		return nil, errors.New("no Google Sheet imported yet")
	}
	p, err := s.analyzeSheet(context.Background(), SheetRequest{
		URL: st.SheetURL, DefaultHost: st.SheetDefaultHost, DefaultUser: st.SheetDefaultUser,
		Folder: st.SheetFolder, UseAI: st.SheetUseAI,
	})
	if err != nil {
		return nil, err
	}
	r := &RefreshResult{Diff: p.Diff, Method: p.Method, Sessions: len(p.Result.Sessions), Skipped: len(p.Result.Skipped)}

	prefix := store.CleanFolder(st.SheetFolder)
	if prefix != "" {
		inSheet := map[string]bool{}
		for _, sess := range p.Result.Sessions {
			f := prefix
			if sf := store.CleanFolder(sess.Folder); sf != "" {
				f += "/" + sf
			}
			inSheet[f+"\x00"+sess.Name] = true
		}
		for _, sess := range s.c.Store.List() {
			if sess.Type == store.TypeSSH && (sess.Folder == prefix || strings.HasPrefix(sess.Folder, prefix+"/")) && !inSheet[sess.Folder+"\x00"+sess.Name] {
				r.Missing++
			}
		}
	}

	c, err := s.CommitSessions(p.Result.Sessions, p.Result.Folders, st.SheetFolder)
	if err != nil {
		return nil, err
	}
	r.Added, r.Updated = c.Added, c.Updated
	return r, nil
}

// CommitSessions saves previewed sessions under targetFolder ("" = root).
// Sessions that already exist there with the same name have their host, port
// and user updated instead of being duplicated.
func (s *ImportService) CommitSessions(sessions []*store.Session, folders []string, targetFolder string) (ImportCount, error) {
	prefix := store.CleanFolder(targetFolder)
	join := func(f string) string {
		f = store.CleanFolder(f)
		switch {
		case prefix == "":
			return f
		case f == "":
			return prefix
		}
		return prefix + "/" + f
	}
	if prefix != "" {
		if err := s.c.Store.AddFolder(prefix); err != nil {
			return ImportCount{}, err
		}
	}
	for _, f := range folders {
		if err := s.c.Store.AddFolder(join(f)); err != nil {
			return ImportCount{}, err
		}
	}
	list := make([]*store.Session, 0, len(sessions))
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		sess.Folder = join(sess.Folder)
		list = append(list, sess)
	}
	added, updated, err := s.c.Store.UpsertSSH(list)
	if err != nil {
		return ImportCount{}, err
	}
	s.c.Emit("sessions:changed", nil)
	return ImportCount{Added: added, Updated: updated}, nil
}

// HasAIKey reports whether an API key is stored for provider
// ("anthropic" or "openrouter"; "" = anthropic).
func (s *ImportService) HasAIKey(provider string) bool {
	name, err := aiKeySecret(provider)
	return err == nil && s.c.Secrets.Has(name)
}

// SetAIKey stores a provider's API key in the OS credential store ("" removes it).
func (s *ImportService) SetAIKey(provider, key string) error {
	name, err := aiKeySecret(provider)
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return s.c.Secrets.Delete(name)
	}
	return s.c.Secrets.Set(name, key)
}
