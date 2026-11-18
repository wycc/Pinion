// Package sheets imports SSH sessions from a Google Sheets spreadsheet (or any
// CSV URL). The sheet is downloaded as CSV through Google's export endpoint, so
// it must be readable without signing in ("Anyone with the link can view", or
// published to the web).
//
// Rows are turned into sessions either by Parse (header keyword rules plus
// pattern matching such as "ssh -p 2222 user@host") or, when an API key is set,
// by ParseAI, which asks Claude to read the table.
package sheets

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ka1hung/pinion/internal/importer/bookmarks"
	"github.com/ka1hung/pinion/internal/store"
)

// Options fill in values the sheet does not provide.
type Options struct {
	DefaultHost string `json:"defaultHost"` // used for rows without a host (e.g. one gateway, many ports)
	DefaultUser string `json:"defaultUser"`
}

// Entry is one connection read from the sheet, before defaults are applied.
type Entry struct {
	Name   string `json:"name"`
	Folder string `json:"folder"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	User   string `json:"user"`
	Row    int    `json:"row"` // sheet row the entry came from (set by the AI path, used for caching)
}

// ErrNotShared is returned when Google answers with a sign-in page.
var ErrNotShared = errors.New(`the sheet is not readable without signing in: in Google Sheets use Share → General access → "Anyone with the link" (Viewer), or File → Share → Publish to web`)

var (
	reDocID  = regexp.MustCompile(`/spreadsheets/d/([A-Za-z0-9_-]+)`)
	rePubID  = regexp.MustCompile(`/spreadsheets/d/e/([A-Za-z0-9_-]+)`)
	reBareID = regexp.MustCompile(`^[A-Za-z0-9_-]{25,}$`)
	reGID    = regexp.MustCompile(`gid=(\d+)`)
)

// ExportURL turns a Google Sheets link (edit, view, published, or a bare
// spreadsheet ID) into a CSV download URL. Other http(s) URLs are returned
// unchanged so a plain CSV link also works.
func ExportURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("enter a Google Sheets link")
	}
	if reBareID.MatchString(raw) {
		return "https://docs.google.com/spreadsheets/d/" + raw + "/export?format=csv", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("not a valid link: %q", raw)
	}
	gid := ""
	if m := reGID.FindStringSubmatch(u.RawQuery + "&" + u.Fragment); m != nil {
		gid = m[1]
	}
	if u.Host == "docs.google.com" {
		if m := rePubID.FindStringSubmatch(u.Path); m != nil {
			out := "https://docs.google.com/spreadsheets/d/e/" + m[1] + "/pub?output=csv"
			if gid != "" {
				out += "&gid=" + gid
			}
			return out, nil
		}
		if m := reDocID.FindStringSubmatch(u.Path); m != nil {
			out := "https://docs.google.com/spreadsheets/d/" + m[1] + "/export?format=csv"
			if gid != "" {
				out += "&gid=" + gid
			}
			return out, nil
		}
		return "", errors.New("this Google link is not a spreadsheet")
	}
	return raw, nil
}

const maxCSV = 16 << 20

// Fetch downloads the sheet and returns its cells.
func Fetch(ctx context.Context, link string) ([][]string, error) {
	u, err := ExportURL(link)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Pinion")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrNotShared
	case resp.StatusCode == http.StatusNotFound:
		return nil, errors.New("sheet not found (check the link and the tab)")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return nil, ErrNotShared
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxCSV+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxCSV {
		return nil, errors.New("sheet is too large")
	}
	return ParseCSV(b)
}

// ParseCSV reads CSV bytes into rows (ragged rows allowed, BOM stripped).
func ParseCSV(b []byte) ([][]string, error) {
	s := strings.TrimPrefix(string(b), "\uFEFF")
	r := csv.NewReader(strings.NewReader(s))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("not a CSV sheet: %w", err)
	}
	return rows, nil
}

// ---- rule-based parsing ----

type colKind int

const (
	colNone colKind = iota
	colName
	colHost
	colPort
	colUser
	colFolder
)

func normHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer(" ", "", "_", "", "-", "", ".", "", "　", "", "(", "", ")", "", "（", "", "）", "").Replace(s)
}

// classify guesses what a header cell holds and how sure we are (higher wins).
func classify(h string) (colKind, int) {
	h = normHeader(h)
	if h == "" {
		return colNone, 0
	}
	has := func(subs ...string) bool {
		for _, x := range subs {
			if strings.Contains(h, x) {
				return true
			}
		}
		return false
	}
	switch {
	case has("sshport", "ssh埠", "ssh連接埠", "ssh通訊埠"):
		return colPort, 10
	case h == "port" || has("埠", "通訊port", "連線port") || (strings.HasSuffix(h, "port") && !has("portal", "report", "support", "export", "import")):
		return colPort, 8
	case h == "ip" || has("ipaddress", "ip位址", "ip地址", "位址", "地址", "address", "hostname", "fqdn", "domain", "網域", "伺服器", "server") ||
		strings.HasPrefix(h, "ip") && !has("ipad") || strings.HasSuffix(h, "ip") && !has("ship", "skip", "tip"):
		return colHost, 8
	case h == "host" || has("主機位址", "主機ip", "gateway", "跳板"):
		return colHost, 7
	case has("username", "帳號", "帳戶", "account", "login", "使用者帳號", "登入"):
		return colUser, 8
	case h == "user":
		return colUser, 7
	case has("session", "名稱", "主機名稱", "設備", "機器", "機台", "device", "machine", "label", "title"):
		return colName, 8
	case h == "name" || has("名字", "姓名", "owner", "member", "學生", "使用者", "人員", "student"):
		return colName, 6
	case has("folder", "group", "site", "category", "案場", "群組", "分類", "組別", "區域", "location", "專案", "project"):
		return colFolder, 7
	case has("name"):
		return colName, 5
	case has("host", "主機"):
		return colHost, 5
	}
	return colNone, 0
}

type columns map[colKind]int

func detectHeader(row []string) (columns, int) {
	cols := columns{}
	score := map[colKind]int{}
	for i, cell := range row {
		k, s := classify(cell)
		if k == colNone {
			continue
		}
		if s > score[k] {
			cols[k], score[k] = i, s
		}
	}
	return cols, len(cols)
}

var (
	reSSHCmd   = regexp.MustCompile(`(?i)(?:^|\s)ssh\s+(.+)`)
	reUserHost = regexp.MustCompile(`^([A-Za-z0-9._-]+)@([A-Za-z0-9.-]+)(?::(\d{1,5}))?$`)
	reHostPort = regexp.MustCompile(`^([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+):(\d{1,5})$`)
	reHost     = regexp.MustCompile(`^(?:\d{1,3}(?:\.\d{1,3}){3}|[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)+|\[[0-9a-fA-F:]+\]|[0-9a-fA-F:]*:[0-9a-fA-F:]+)$`)
	reInt      = regexp.MustCompile(`\d{1,5}`)
)

func parsePort(s string) int {
	m := reInt.FindString(s)
	if m == "" {
		return 0
	}
	n, _ := strconv.Atoi(m)
	if n < 1 || n > 65535 {
		return 0
	}
	return n
}

func validHost(h string) bool { return reHost.MatchString(h) }

// scanCell extracts host/port/user from free text like "ssh -p 2222 me@h",
// "me@h:2222" or "h:2222".
func scanCell(s string, e *Entry) {
	s = strings.TrimSpace(s)
	if m := reSSHCmd.FindStringSubmatch(s); m != nil {
		scanSSHArgs(strings.Fields(m[1]), e)
		return
	}
	if m := reUserHost.FindStringSubmatch(s); m != nil {
		if e.Host == "" {
			e.Host = m[2]
		}
		if e.User == "" {
			e.User = m[1]
		}
		if e.Port == 0 && m[3] != "" {
			e.Port = parsePort(m[3])
		}
		return
	}
	if m := reHostPort.FindStringSubmatch(s); m != nil {
		if e.Host == "" {
			e.Host = m[1]
		}
		if e.Port == 0 {
			e.Port = parsePort(m[2])
		}
	}
}

// scanSSHArgs reads `[-p N] [-l user] [user@]host [-p N]` from ssh arguments.
func scanSSHArgs(args []string, e *Entry) {
	var host, user string
	port := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-p" || a == "-l":
			if i+1 < len(args) {
				if a == "-p" {
					port = parsePort(args[i+1])
				} else {
					user = args[i+1]
				}
				i++
			}
		case strings.HasPrefix(a, "-p") && len(a) > 2:
			port = parsePort(a[2:])
		case strings.HasPrefix(a, "-"):
			// other option; skip its value for options that take one
			if len(a) == 2 && strings.Contains("bcDEeFIiJLmOoQRSWw", a[1:]) {
				i++
			}
		case host == "":
			if m := reUserHost.FindStringSubmatch(a); m != nil {
				user, host = m[1], m[2]
				if m[3] != "" && port == 0 {
					port = parsePort(m[3])
				}
			} else if validHost(a) {
				host = a
			}
		}
	}
	if e.Host == "" {
		e.Host = strings.Trim(host, "[]")
	}
	if e.User == "" {
		e.User = user
	}
	if e.Port == 0 {
		e.Port = port
	}
}

func cell(row []string, i int, ok bool) string {
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func blank(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// Layout is what Extract learns from the sheet's header: which column holds
// what. It lets single rows be analysed on their own (see Analyze).
type Layout struct {
	Header int // index of the header row; -1 = no header (free-text scan)
	cols   columns
}

// Row is one non-blank data row with the context it needs to be analysed alone.
type Row struct {
	Index     int      // position in the sheet (0-based)
	Cells     []string //
	Inherited string   // name of the site above, for rows with an empty name cell
}

// DetectLayout finds the header row and assigns columns.
func DetectLayout(rows [][]string) (*Layout, []string) {
	for i := 0; i < len(rows) && i < 15; i++ {
		c, n := detectHeader(rows[i])
		_, hasPort := c[colPort]
		_, hasHost := c[colHost]
		if (hasPort || hasHost) && (n >= 2 || hasPort) {
			l := &Layout{Header: i, cols: c}
			body := rows[i+1:]
			// A "Server" or "Address" column full of notes is not a host column.
			if hi, ok := c[colHost]; ok && !mostlyHosts(body, hi) {
				delete(c, colHost)
			}
			// Without a recognised name header, the leftmost column that is not
			// otherwise used is the name (e.g. a site column titled with a project name).
			if _, ok := c[colName]; !ok {
				if fi, ok := c[colFolder]; ok {
					// a "site"/"group" column is the best name a sheet without a
					// name column has
					c[colName] = fi
					delete(c, colFolder)
				}
			}
			if _, ok := c[colName]; !ok {
				used := map[int]bool{}
				for _, ci := range c {
					used[ci] = true
				}
				for ci := 0; ci < len(rows[i]); ci++ {
					if !used[ci] {
						c[colName] = ci
						break
					}
				}
			}
			return l, nil
		}
	}
	return &Layout{Header: -1, cols: columns{}},
		[]string{"no header row with a host/IP or port column was found; looking for ssh commands and host:port text instead"}
}

// HeaderCells returns the header row ("" cells if there is none).
func (l *Layout) HeaderCells(rows [][]string) []string {
	if l.Header < 0 || l.Header >= len(rows) {
		return nil
	}
	return rows[l.Header]
}

// DataRows lists the non-blank rows below the header, each with the name of
// the site above it for continuation rows.
func (l *Layout) DataRows(rows [][]string) []Row {
	var out []Row
	ni, hasName := l.cols[colName]
	last := ""
	for i := l.Header + 1; i < len(rows); i++ {
		if blank(rows[i]) {
			continue
		}
		r := Row{Index: i, Cells: rows[i]}
		if l.Header >= 0 && hasName {
			if n := oneLine(cell(rows[i], ni, true)); n != "" {
				last = n
			} else {
				r.Inherited = last
			}
		}
		out = append(out, r)
	}
	return out
}

// ExtractRow reads the entries of one data row.
//
// Sheets kept by hand rarely hold one clean value per cell, so with a port
// column every port in the cell becomes its own entry: "2222, 3333",
// "7100/7200", "TR: 6810" on one line and "VS4: 6820" on the next, or
// "6010改6000" (changed to 6000). Notes in brackets such as "(old:…)" or
// "(跟鴻匯衝)" are ignored. When a row lists several ports, or labels one, the
// session name gets the label or port appended.
func (l *Layout) ExtractRow(r Row) []Entry {
	row := r.Cells
	if l.Header < 0 {
		return extractFreeText(row)
	}
	cols := l.cols
	portCol, hasPortCol := cols[colPort]
	_, hasHostCol := cols[colHost]

	var base Entry
	i, ok := cols[colName]
	base.Name = oneLine(cell(row, i, ok))
	inherited := false
	if base.Name == "" && r.Inherited != "" {
		base.Name, inherited = r.Inherited, true // an extra device of the site above
	}
	i, ok = cols[colFolder]
	base.Folder = oneLine(cell(row, i, ok))
	i, ok = cols[colUser]
	base.User = cell(row, i, ok)
	i, ok = cols[colHost]
	if h := cell(row, i, ok); h != "" {
		if validHost(h) {
			base.Host = h
		} else {
			scanCell(h, &base)
		}
	}

	if !hasPortCol {
		// host column only: one entry per row, port from free text if any
		if !hasHostCol || base.Host == "" {
			for _, c := range row {
				scanCell(c, &base)
			}
		}
		if base.Host != "" {
			return []Entry{base}
		}
		return nil
	}

	items := splitPorts(cell(row, portCol, true))
	if len(items) == 0 {
		if base.Host != "" && base.Port == 0 {
			return []Entry{base} // host without a port: default 22
		}
		return nil
	}
	var out []Entry
	for _, it := range items {
		e := base
		if base.Port == 0 || len(items) > 1 {
			e.Port = it.Port
		}
		switch {
		case it.Label != "" && len(items) > 1:
			e.Name = fmt.Sprintf("%s (%s %d)", base.Name, it.Label, it.Port)
		case it.Label != "":
			e.Name = fmt.Sprintf("%s (%s)", base.Name, it.Label)
		case len(items) > 1 || inherited:
			e.Name = fmt.Sprintf("%s (%d)", base.Name, it.Port)
		}
		e.Name = strings.TrimSpace(e.Name)
		out = append(out, e)
	}
	return out
}

// Extract reads entries from the rows using header keywords and patterns.
func Extract(rows [][]string) (entries []Entry, warnings []string) {
	l, warnings := DetectLayout(rows)
	for _, r := range l.DataRows(rows) {
		entries = append(entries, l.ExtractRow(r)...)
	}
	return entries, warnings
}

// extractFreeText handles a row of a sheet without a usable header: it is
// scanned for ssh commands, user@host and host:port text.
func extractFreeText(row []string) []Entry {
	var e Entry
	for _, c := range row {
		scanCell(c, &e)
	}
	if e.Host == "" {
		return nil // without a header a lone number is not reliably a port
	}
	// the first short non-address cell is a good label
	for _, c := range row {
		c = strings.TrimSpace(c)
		if c != "" && !strings.ContainsAny(c, "@:") && !validHost(c) && parsePort(c) == 0 && len([]rune(c)) <= 40 {
			e.Name = c
			break
		}
	}
	return []Entry{e}
}

// mostlyHosts reports whether at least half of the filled cells of column i
// are a bare host name or IP address.
func mostlyHosts(rows [][]string, i int) bool {
	filled, hosts := 0, 0
	for _, r := range rows {
		c := cell(r, i, true)
		if c == "" {
			continue
		}
		filled++
		if validHost(c) {
			hosts++
		}
	}
	return filled > 0 && hosts*2 >= filled
}

// oneLine joins a multi-line cell into one line with single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

type portItem struct {
	Port  int
	Label string
}

var (
	reParen   = regexp.MustCompile(`[(（]([^()（）]*)[)）]`)
	reChanged = regexp.MustCompile(`\d+\s*(?:改|->|→|=>)\s*(\d+)`)
	rePortSep = regexp.MustCompile(`[,，/、;；|]`)
	reDigits  = regexp.MustCompile(`\d+`)
	// words marking a note rather than a label
	noteWords = []string{"old", "原", "舊", "衝", "重覆", "重複", "換", "改", "與", "跟", "修改", "?", "？"}
)

func isNote(s string) bool {
	l := strings.ToLower(s)
	for _, w := range noteWords {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

func isASCIIAlnum(r byte) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// findPorts returns the standalone 2–5 digit numbers in s (not part of a word
// such as "4ch" or "i5", an IP address, or a host:port pair), with their
// positions.
func findPorts(s string) (ports []int, spans [][2]int) {
	for _, m := range reDigits.FindAllStringIndex(s, -1) {
		a, b := m[0], m[1]
		if b-a < 2 || b-a > 5 {
			continue
		}
		if a > 0 {
			p := s[a-1]
			if isASCIIAlnum(p) || p == '.' || p == '_' || p == '-' {
				continue
			}
			if p == ':' && a > 1 && s[a-2] >= '0' && s[a-2] <= '9' { // x.x.x.x:port
				continue
			}
		}
		if b < len(s) {
			n := s[b]
			if isASCIIAlnum(n) || n == '.' || n == '_' {
				continue
			}
		}
		v, _ := strconv.Atoi(s[a:b])
		if v < 1 || v > 65535 {
			continue
		}
		ports = append(ports, v)
		spans = append(spans, [2]int{a, b})
	}
	return ports, spans
}

func cleanLabel(s string) string {
	s = strings.Trim(oneLine(s), " :：-–—=#.?？")
	if s == "" || isNote(s) || len([]rune(s)) > 20 {
		return ""
	}
	return s
}

// splitPorts reads every port in a port cell, with an optional label each.
func splitPorts(text string) []portItem {
	var out []portItem
	seen := map[int]bool{}
	add := func(p int, label string) {
		if seen[p] {
			return
		}
		seen[p] = true
		out = append(out, portItem{Port: p, Label: label})
	}
	for _, line := range strings.Split(text, "\n") {
		// bracketed text is a label like "(i5 VM)" or a note like "(old:…)";
		// numbers inside never count as ports
		parenLabel := ""
		line = reParen.ReplaceAllStringFunc(line, func(m string) string {
			if inner := cleanLabel(reParen.FindStringSubmatch(m)[1]); parenLabel == "" && inner != "" {
				parenLabel = inner
			}
			return " "
		})
		line = reChanged.ReplaceAllString(line, "$1") // "6010改6000" → "6000"
		start := len(out)
		for _, piece := range rePortSep.Split(line, -1) {
			ports, spans := findPorts(piece)
			switch len(ports) {
			case 0:
				continue
			case 1:
				label := cleanLabel(piece[:spans[0][0]] + " " + piece[spans[0][1]:])
				if strings.EqualFold(label, "id") {
					continue // "ID:9130" is a device ID, not a port
				}
				add(ports[0], label)
			default:
				// "4ch:7940 TR:7950": one label per whitespace-separated token
				for _, tok := range strings.Fields(piece) {
					p, sp := findPorts(tok)
					if len(p) == 1 {
						add(p[0], cleanLabel(tok[:sp[0][0]]+" "+tok[sp[0][1]:]))
					}
				}
			}
		}
		if parenLabel != "" && len(out) == start+1 && out[start].Label == "" {
			out[start].Label = parenLabel
		}
	}
	return out
}

// Build applies defaults and turns entries into sessions.
func Build(entries []Entry, opt Options) *bookmarks.Result {
	res := &bookmarks.Result{Encoding: "UTF-8", Sessions: []*store.Session{}, Folders: []string{}, Skipped: []bookmarks.Skipped{}, Warnings: []string{}}
	seen := map[string]int{}
	folders := map[string]bool{}
	defHost := strings.TrimSpace(opt.DefaultHost)
	defUser := strings.TrimSpace(opt.DefaultUser)
	for n, e := range entries {
		e.Name = strings.TrimSpace(e.Name)
		e.Host = strings.TrimSpace(e.Host)
		e.User = strings.TrimSpace(e.User)
		e.Folder = store.CleanFolder(strings.ReplaceAll(e.Folder, "\\", "/"))
		if e.Host == "" {
			e.Host = defHost
		}
		if e.User == "" {
			e.User = defUser
		}
		label := e.Name
		if label == "" {
			label = fmt.Sprintf("row %d", n+1)
		}
		switch {
		case e.Host == "":
			res.Skipped = append(res.Skipped, bookmarks.Skipped{Name: label, Folder: e.Folder, Type: "SSH", Reason: "no host — fill in the relay server"})
			continue
		case !validHost(e.Host):
			res.Skipped = append(res.Skipped, bookmarks.Skipped{Name: label, Folder: e.Folder, Type: "SSH", Reason: "invalid host " + strconv.Quote(e.Host)})
			continue
		case e.Port < 0 || e.Port > 65535:
			res.Skipped = append(res.Skipped, bookmarks.Skipped{Name: label, Folder: e.Folder, Type: "SSH", Reason: "invalid port"})
			continue
		}
		if e.Port == 0 {
			e.Port = 22
		}
		if e.Name == "" {
			e.Name = e.Host
			if e.Port != 22 {
				e.Name += ":" + strconv.Itoa(e.Port)
			}
		}
		key := e.Folder + "\x00" + e.Name
		if seen[key]++; seen[key] > 1 {
			e.Name = fmt.Sprintf("%s (%d)", e.Name, seen[key])
		}
		if e.Folder != "" && !folders[e.Folder] {
			folders[e.Folder] = true
			res.Folders = append(res.Folders, e.Folder)
		}
		s := &store.Session{
			Name:   e.Name,
			Folder: e.Folder,
			Type:   store.TypeSSH,
			SSH:    &store.SSHConfig{Host: e.Host, Port: e.Port, User: e.User, Auth: store.AuthAuto, SFTPAutoOpen: true},
		}
		s.Normalize()
		res.Sessions = append(res.Sessions, s)
	}
	return res
}

// Parse is Extract followed by Build.
func Parse(rows [][]string, opt Options) *bookmarks.Result {
	entries, warn := Extract(rows)
	res := Build(entries, opt)
	res.Warnings = append(warn, res.Warnings...)
	return res
}

// ToCSV renders rows back to CSV text (for the AI prompt), dropping blank rows
// and trailing empty cells.
func ToCSV(rows [][]string) string {
	var sb strings.Builder
	w := csv.NewWriter(&sb)
	for _, r := range rows {
		if blank(r) {
			continue
		}
		end := len(r)
		for end > 0 && strings.TrimSpace(r[end-1]) == "" {
			end--
		}
		_ = w.Write(r[:end])
	}
	w.Flush()
	return sb.String()
}
