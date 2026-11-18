package sheets

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func siteRows() [][]string {
	return [][]string{
		{"Sites", "SSH port"},
		{"A", "2001, 2002"},
		{"B", "3001"},
		{"", "3002"}, // continuation of B
		{"C", "4001"},
	}
}

func TestAnalyzeRulesOnlyChangedRows(t *testing.T) {
	ctx := context.Background()
	es, d, c, _, err := Analyze(ctx, siteRows(), MethodRules, nil, nil)
	if err != nil || len(es) != 5 || d.Changed != 4 || d.Rows != 4 || d.Removed != 0 {
		t.Fatalf("first run: %d entries, diff %+v, err %v", len(es), d, err)
	}
	// round-trip the cache through JSON like the app does
	b, _ := json.Marshal(c)
	var c2 Cache
	_ = json.Unmarshal(b, &c2)

	_, d, c3, _, _ := Analyze(ctx, siteRows(), MethodRules, &c2, nil)
	if d.Changed != 0 || d.Removed != 0 {
		t.Fatalf("unchanged sheet: diff %+v", d)
	}

	rows := siteRows()
	rows[2][0] = "B2" // renaming B also changes the continuation row below it
	es, d, _, _, _ = Analyze(ctx, rows, MethodRules, c3, nil)
	if d.Changed != 2 || d.Removed != 2 {
		t.Fatalf("after rename: diff %+v", d)
	}
	if es[3].Name != "B2 (3002)" {
		t.Errorf("continuation name %q", es[3].Name)
	}

	// another method re-analyses everything
	_, d, _, _, _ = Analyze(ctx, siteRows(), "ai:x", c3, func(context.Context, string) ([]Entry, error) { return nil, nil })
	if d.Changed != 4 {
		t.Errorf("method switch: diff %+v", d)
	}
}

func TestAnalyzeAISendsOnlyChangedRows(t *testing.T) {
	ctx := context.Background()
	_, _, c, _, _ := Analyze(ctx, siteRows(), "ai:test", nil, func(_ context.Context, text string) ([]Entry, error) {
		return []Entry{{Row: 2, Name: "A", Port: 2001}, {Row: 2, Name: "A", Port: 2002}, {Row: 3, Name: "B", Port: 3001}, {Row: 4, Name: "B (3002)", Port: 3002}, {Row: 5, Name: "C", Port: 4001}}, nil
	})
	rows := siteRows()
	rows[4][1] = "4009"
	var sent string
	es, d, _, _, err := Analyze(ctx, rows, "ai:test", c, func(_ context.Context, text string) ([]Entry, error) {
		sent = text
		return []Entry{{Row: 5, Name: "C", Port: 4009}}, nil
	})
	if err != nil || d.Changed != 1 {
		t.Fatalf("diff %+v err %v", d, err)
	}
	if !strings.Contains(sent, "#,Sites,SSH port,context") || !strings.Contains(sent, "5,C,4009") || strings.Contains(sent, "2001") {
		t.Errorf("sent to AI:\n%s", sent)
	}
	if len(es) != 5 || es[4].Port != 4009 || es[0].Port != 2001 {
		t.Errorf("entries %+v", es)
	}
}

func TestAnalyzeAIChunks(t *testing.T) {
	old := AIChunkRows
	AIChunkRows = 2
	defer func() { AIChunkRows = old }()
	var mu sync.Mutex
	calls := 0
	es, d, _, _, err := Analyze(context.Background(), siteRows(), "ai:test", nil, func(_ context.Context, text string) ([]Entry, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		if !strings.HasPrefix(text, "#,Sites,SSH port,context") {
			t.Errorf("chunk without header:\n%s", text)
		}
		var out []Entry
		for _, line := range strings.Split(strings.TrimSpace(text), "\n")[1:] {
			f := strings.Split(line, ",")
			n, _ := strconv.Atoi(f[0])
			out = append(out, Entry{Row: n, Name: f[1], Port: 1})
		}
		return out, nil
	})
	if err != nil || calls != 2 || d.Changed != 4 || len(es) != 4 {
		t.Fatalf("calls %d diff %+v entries %d err %v", calls, d, len(es), err)
	}
	for i, want := range []int{2, 3, 4, 5} {
		if es[i].Row != want {
			t.Errorf("entry %d from row %d, want %d", i, es[i].Row, want)
		}
	}
	// a failing chunk fails the analysis
	_, _, _, _, err = Analyze(context.Background(), siteRows(), "ai:test", nil, func(context.Context, string) ([]Entry, error) {
		return nil, errors.New("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Errorf("err %v", err)
	}
}
