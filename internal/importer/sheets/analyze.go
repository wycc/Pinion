package sheets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Cache remembers the entries found in each row of the last analysed version
// of a sheet, so a refresh only analyses rows that changed.
type Cache struct {
	URL  string             `json:"url"`
	Rows map[string][]Entry `json:"rows"` // row key → entries found in that row
	Keys []string           `json:"keys"` // row keys of the last version, in sheet order
}

// Diff summarises how the sheet changed since the cached version.
type Diff struct {
	Rows    int `json:"rows"`    // data rows in the sheet
	Changed int `json:"changed"` // rows analysed this time (new or modified)
	Removed int `json:"removed"` // rows of the previous version that are gone
}

// AIFunc analyses rows rendered by aiRows and returns entries tagged with the
// row number ("#" column) they came from.
type AIFunc func(ctx context.Context, text string) ([]Entry, error)

// MethodRules is the method name for the built-in rules.
const MethodRules = "rules"

// parserVersion is part of every row key: bump it whenever the rules or the
// AI prompt change, so cached results from older code are not reused.
const parserVersion = "3"

// rowKey identifies a row's content in context: the analysis method, the
// header (column meanings), the cells, and the site name inherited from the
// row above. Any change to these makes the row "changed".
func rowKey(method string, header []string, r Row) string {
	h := sha256.New()
	h.Write([]byte(parserVersion))
	h.Write([]byte{0x1e})
	write := func(cells []string) {
		end := len(cells)
		for end > 0 && strings.TrimSpace(cells[end-1]) == "" {
			end--
		}
		for _, c := range cells[:end] {
			h.Write([]byte(strings.TrimSpace(c)))
			h.Write([]byte{0x1f})
		}
		h.Write([]byte{0x1e})
	}
	h.Write([]byte(method))
	h.Write([]byte{0x1e})
	write(header)
	write(r.Cells)
	h.Write([]byte(r.Inherited))
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// Analyze extracts the entries of every row, reusing cached results for rows
// that did not change and analysing only the others — with the built-in rules
// when ai is nil, otherwise with one AI request for all changed rows. method
// names the analysis ("rules", or the AI provider and model) so switching
// method re-analyses everything. It returns the entries in sheet order and
// the cache for the next run.
func Analyze(ctx context.Context, rows [][]string, method string, cache *Cache, ai AIFunc) ([]Entry, Diff, *Cache, []string, error) {
	l, warnings := DetectLayout(rows)
	header := l.HeaderCells(rows)
	data := l.DataRows(rows)

	old := map[string][]Entry{}
	oldKeys := map[string]int{}
	if cache != nil {
		if cache.Rows != nil {
			old = cache.Rows
		}
		for _, k := range cache.Keys {
			oldKeys[k]++
		}
	}

	keys := make([]string, len(data))
	var need []Row
	needKey := map[int]string{}
	for i, r := range data {
		keys[i] = rowKey(method, header, r)
		if _, ok := old[keys[i]]; !ok {
			if _, dup := needKey[r.Index]; !dup {
				need = append(need, r)
				needKey[r.Index] = keys[i]
			}
		}
	}

	fresh := map[string][]Entry{}
	if len(need) > 0 {
		if ai == nil {
			for _, r := range need {
				fresh[needKey[r.Index]] = l.ExtractRow(r)
			}
		} else {
			got, err := aiChunks(ctx, header, need, ai)
			if err != nil {
				return nil, Diff{}, nil, nil, err
			}
			byRow := map[int][]Entry{}
			for _, e := range got {
				byRow[e.Row] = append(byRow[e.Row], e)
			}
			for _, r := range need {
				fresh[needKey[r.Index]] = byRow[r.Index+1] // "#" is 1-based like the sheet
			}
		}
	}

	next := &Cache{Rows: map[string][]Entry{}, Keys: keys}
	var entries []Entry
	newKeys := map[string]int{}
	for i, r := range data {
		k := keys[i]
		newKeys[k]++
		es, ok := fresh[k]
		if !ok {
			es = old[k]
		}
		if es == nil {
			es = []Entry{}
		}
		next.Rows[k] = es
		for _, e := range es {
			e.Row = r.Index + 1
			entries = append(entries, e)
		}
	}

	d := Diff{Rows: len(data), Changed: len(need)}
	for k, n := range oldKeys {
		if m := newKeys[k]; n > m {
			d.Removed += n - m
		}
	}
	return entries, d, next, warnings, nil
}

// AIChunkRows is how many rows go into one AI request. Small requests keep
// smaller models focused (a whole sheet at once can exhaust their output on
// reasoning) and let several run in parallel.
var AIChunkRows = 25

// aiParallel limits concurrent AI requests.
const aiParallel = 4

// aiChunks sends the rows to the AI in chunks of AIChunkRows, each with the
// header, and returns all entries. Any failed chunk fails the whole call.
func aiChunks(ctx context.Context, header []string, rows []Row, ai AIFunc) ([]Entry, error) {
	size := AIChunkRows
	if size <= 0 {
		size = len(rows)
	}
	var chunks [][]Row
	for i := 0; i < len(rows); i += size {
		chunks = append(chunks, rows[i:min(i+size, len(rows))])
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([][]Entry, len(chunks))
	errs := make([]error, len(chunks))
	sem := make(chan struct{}, aiParallel)
	var wg sync.WaitGroup
	for i, ch := range chunks {
		text, err := aiRows(header, ch)
		if err != nil {
			return nil, err
		}
		wg.Add(1)
		go func(i int, text string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				errs[i] = ctx.Err()
				return
			}
			results[i], errs[i] = ai(ctx, text)
			if errs[i] != nil {
				cancel() // no point finishing the others
			}
		}(i, text)
	}
	wg.Wait()
	var out []Entry
	for i := range chunks {
		if errs[i] != nil && !errors.Is(errs[i], context.Canceled) {
			return nil, errs[i]
		}
	}
	for i := range chunks {
		if errs[i] != nil {
			return nil, errs[i]
		}
		out = append(out, results[i]...)
	}
	return out, nil
}

// aiRows renders the rows to analyse as CSV for the AI: a "#" column with the
// sheet row number first and, for rows whose name cell is empty, a "context"
// column naming the site above. The header row is included when known.
func aiRows(header []string, rows []Row) (string, error) {
	width := len(header)
	for _, r := range rows {
		if len(r.Cells) > width {
			width = len(r.Cells)
		}
	}
	pad := func(c []string) []string {
		out := make([]string, width)
		copy(out, c)
		return out
	}
	var table [][]string
	if header != nil {
		table = append(table, append(append([]string{"#"}, pad(header)...), "context"))
	}
	for _, r := range rows {
		ctx := ""
		if r.Inherited != "" {
			ctx = "site above: " + r.Inherited
		}
		table = append(table, append(append([]string{strconv.Itoa(r.Index + 1)}, pad(r.Cells)...), ctx))
	}
	text := ToCSV(table)
	if len(text) > maxAIInput {
		return "", fmt.Errorf("too many changed rows for one AI request (%d KB, limit %d KB); turn AI off to use the built-in rules", len(text)/1024, maxAIInput/1024)
	}
	return text, nil
}
