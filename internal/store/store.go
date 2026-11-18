// Package store persists sessions and settings as JSON files in the user's
// config directory. Nothing secret is written here.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Data is the on-disk sessions file.
type Data struct {
	Version  int        `json:"version"`
	Folders  []string   `json:"folders"` // explicit folders (may be empty)
	Sessions []*Session `json:"sessions"`
}

// Store is safe for concurrent use.
type Store struct {
	dir string

	mu       sync.RWMutex
	data     Data
	settings Settings
}

// DefaultDir returns the per-user config dir for Pinion.
func DefaultDir() (string, error) {
	if d := os.Getenv("PINION_CONFIG_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Pinion"), nil
}

// Open loads (or initializes) the store in dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, data: Data{Version: 1}, settings: DefaultSettings()}
	if err := readJSON(filepath.Join(dir, "sessions.json"), &s.data); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("sessions.json: %w", err)
	}
	if err := readJSON(filepath.Join(dir, "settings.json"), &s.settings); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("settings.json: %w", err)
	}
	if s.settings.TerminalFont == legacyTerminalFont {
		s.settings.TerminalFont = ""
	}
	if s.settings.EditorFont == legacyEditorFont {
		s.settings.EditorFont = ""
	}
	if s.settings.LogDir == "" {
		home, _ := os.UserHomeDir()
		s.settings.LogDir = filepath.Join(home, "Pinion", "logs")
	}
	if s.settings.ServerRoot == "" {
		home, _ := os.UserHomeDir()
		s.settings.ServerRoot = filepath.Join(home, "Pinion", "share")
	}
	for _, sess := range s.data.Sessions {
		sess.Normalize()
	}
	return s, nil
}

// Dir is the config directory.
func (s *Store) Dir() string { return s.dir }

// KnownHostsPath is the OpenSSH-compatible known_hosts file used by Pinion.
func (s *Store) KnownHostsPath() string { return filepath.Join(s.dir, "known_hosts") }

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) saveLocked() error {
	return writeJSONAtomic(filepath.Join(s.dir, "sessions.json"), s.data)
}

// NewID returns a random 16-hex-char identifier.
func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// CleanFolder normalizes a folder path ("a\b/ c " -> "a/b/c").
func CleanFolder(f string) string {
	f = strings.ReplaceAll(f, "\\", "/")
	parts := strings.Split(f, "/")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

func clone(s *Session) *Session {
	b, _ := json.Marshal(s)
	var c Session
	_ = json.Unmarshal(b, &c)
	return &c
}

// List returns copies of all sessions sorted by folder then name.
func (s *Store) List() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Session, 0, len(s.data.Sessions))
	for _, x := range s.data.Sessions {
		out = append(out, clone(x))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Folder != out[j].Folder {
			return out[i].Folder < out[j].Folder
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Folders returns all folders: explicit ones plus those implied by sessions
// (including ancestors), sorted.
func (s *Store) Folders() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := map[string]bool{}
	add := func(f string) {
		f = CleanFolder(f)
		for f != "" {
			set[f] = true
			i := strings.LastIndex(f, "/")
			if i < 0 {
				break
			}
			f = f[:i]
		}
	}
	for _, f := range s.data.Folders {
		add(f)
	}
	for _, x := range s.data.Sessions {
		add(x.Folder)
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Get returns a copy of a session.
func (s *Store) Get(id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, x := range s.data.Sessions {
		if x.ID == id {
			return clone(x), nil
		}
	}
	return nil, fmt.Errorf("session %q not found", id)
}

// Save inserts or updates a session and returns the stored copy.
func (s *Store) Save(in *Session) (*Session, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, errors.New("session name is required")
	}
	c := clone(in)
	c.Folder = CleanFolder(c.Folder)
	c.Normalize()
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == "" {
		c.ID = NewID()
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if c.SSH != nil {
		for i := range c.SSH.Tunnels {
			if c.SSH.Tunnels[i].ID == "" {
				c.SSH.Tunnels[i].ID = NewID()
			}
		}
	}
	replaced := false
	for i, x := range s.data.Sessions {
		if x.ID == c.ID {
			if c.CreatedAt.IsZero() {
				c.CreatedAt = x.CreatedAt
			}
			s.data.Sessions[i] = c
			replaced = true
			break
		}
	}
	if !replaced {
		if c.CreatedAt.IsZero() {
			c.CreatedAt = now
		}
		s.data.Sessions = append(s.data.Sessions, c)
	}
	return clone(c), s.saveLocked()
}

// SaveMany inserts sessions in bulk (used by importers). IDs are regenerated.
func (s *Store) SaveMany(list []*Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, in := range list {
		c := clone(in)
		c.ID = NewID()
		c.Folder = CleanFolder(c.Folder)
		c.Normalize()
		c.CreatedAt, c.UpdatedAt = now, now
		s.data.Sessions = append(s.data.Sessions, c)
	}
	return s.saveLocked()
}

// UpsertSSH saves imported SSH sessions. A session whose folder and name match
// an existing SSH session updates that session's host, port and user (other
// settings and saved passwords are kept); the rest are added. Returns how many
// were added and updated.
func (s *Store) UpsertSSH(list []*Session) (added, updated int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	idx := map[string]*Session{}
	for _, x := range s.data.Sessions {
		if x.Type == TypeSSH && x.SSH != nil {
			idx[x.Folder+"\x00"+x.Name] = x
		}
	}
	for _, in := range list {
		c := clone(in)
		c.Folder = CleanFolder(c.Folder)
		c.Normalize()
		if c.Type == TypeSSH {
			if ex := idx[c.Folder+"\x00"+c.Name]; ex != nil {
				if ex.SSH.Host != c.SSH.Host || ex.SSH.Port != c.SSH.Port || (c.SSH.User != "" && ex.SSH.User != c.SSH.User) {
					ex.SSH.Host, ex.SSH.Port = c.SSH.Host, c.SSH.Port
					if c.SSH.User != "" {
						ex.SSH.User = c.SSH.User
					}
					ex.UpdatedAt = now
					updated++
				}
				continue
			}
		}
		c.ID = NewID()
		c.CreatedAt, c.UpdatedAt = now, now
		s.data.Sessions = append(s.data.Sessions, c)
		if c.Type == TypeSSH {
			idx[c.Folder+"\x00"+c.Name] = c
		}
		added++
	}
	return added, updated, s.saveLocked()
}

// Delete removes a session.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, x := range s.data.Sessions {
		if x.ID == id {
			s.data.Sessions = append(s.data.Sessions[:i], s.data.Sessions[i+1:]...)
			return s.saveLocked()
		}
	}
	return fmt.Errorf("session %q not found", id)
}

// Duplicate copies a session with a new ID and " (copy)" suffix.
func (s *Store) Duplicate(id string) (*Session, error) {
	src, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	src.ID = ""
	src.Name += " (copy)"
	if src.SSH != nil {
		for i := range src.SSH.Tunnels {
			src.SSH.Tunnels[i].ID = ""
		}
	}
	return s.Save(src)
}

// AddFolder registers an (empty) folder.
func (s *Store) AddFolder(f string) error {
	f = CleanFolder(f)
	if f == "" {
		return errors.New("folder name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.data.Folders {
		if x == f {
			return nil
		}
	}
	s.data.Folders = append(s.data.Folders, f)
	return s.saveLocked()
}

func underFolder(path, folder string) bool {
	return path == folder || strings.HasPrefix(path, folder+"/")
}

// RenameFolder moves a folder (and its subfolders/sessions) to a new path.
func (s *Store) RenameFolder(from, to string) error {
	from, to = CleanFolder(from), CleanFolder(to)
	if from == "" || to == "" {
		return errors.New("folder name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	re := func(p string) string {
		if underFolder(p, from) {
			return to + p[len(from):]
		}
		return p
	}
	for i, f := range s.data.Folders {
		s.data.Folders[i] = re(f)
	}
	for _, x := range s.data.Sessions {
		x.Folder = re(x.Folder)
	}
	return s.saveLocked()
}

// DeleteFolder removes a folder. If it contains sessions, they are moved to
// the parent folder unless deleteSessions is true.
func (s *Store) DeleteFolder(folder string, deleteSessions bool) error {
	folder = CleanFolder(folder)
	parent := ""
	if i := strings.LastIndex(folder, "/"); i >= 0 {
		parent = folder[:i]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fs := s.data.Folders[:0]
	for _, f := range s.data.Folders {
		if !underFolder(f, folder) {
			fs = append(fs, f)
		}
	}
	s.data.Folders = fs
	ss := s.data.Sessions[:0]
	for _, x := range s.data.Sessions {
		if underFolder(x.Folder, folder) {
			if deleteSessions {
				continue
			}
			x.Folder = parent
		}
		ss = append(ss, x)
	}
	s.data.Sessions = ss
	return s.saveLocked()
}

// MoveSession changes a session's folder.
func (s *Store) MoveSession(id, folder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.data.Sessions {
		if x.ID == id {
			x.Folder = CleanFolder(folder)
			x.UpdatedAt = time.Now()
			return s.saveLocked()
		}
	}
	return fmt.Errorf("session %q not found", id)
}

// Export returns the sessions JSON (no secrets) for the given folder ("" = all).
func (s *Store) Export(folder string) ([]byte, error) {
	folder = CleanFolder(folder)
	s.mu.RLock()
	d := Data{Version: 1}
	for _, f := range s.data.Folders {
		if folder == "" || underFolder(f, folder) {
			d.Folders = append(d.Folders, f)
		}
	}
	for _, x := range s.data.Sessions {
		if folder == "" || underFolder(x.Folder, folder) {
			d.Sessions = append(d.Sessions, x)
		}
	}
	s.mu.RUnlock()
	return json.MarshalIndent(d, "", "  ")
}

// ParseExport decodes a file produced by Export.
func ParseExport(b []byte) (*Data, error) {
	var d Data
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// ImportData merges exported data under an optional target folder prefix.
func (s *Store) ImportData(d *Data, prefix string) (int, error) {
	prefix = CleanFolder(prefix)
	join := func(f string) string {
		f = CleanFolder(f)
		if prefix == "" {
			return f
		}
		if f == "" {
			return prefix
		}
		return prefix + "/" + f
	}
	for _, x := range d.Sessions {
		x.Folder = join(x.Folder)
	}
	s.mu.Lock()
	for _, f := range d.Folders {
		s.data.Folders = append(s.data.Folders, join(f))
	}
	s.mu.Unlock()
	return len(d.Sessions), s.SaveMany(d.Sessions)
}

// Settings returns the current settings.
func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// SaveSettings persists settings.
func (s *Store) SaveSettings(st Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = st
	return writeJSONAtomic(filepath.Join(s.dir, "settings.json"), st)
}
