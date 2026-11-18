package sheets

import (
	"strconv"
	"strings"
	"testing"
)

func TestExportURL(t *testing.T) {
	cases := map[string]string{
		"https://docs.google.com/spreadsheets/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456789abcdefg/edit?pli=1&gid=0#gid=0": "https://docs.google.com/spreadsheets/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456789abcdefg/export?format=csv&gid=0",
		"https://docs.google.com/spreadsheets/d/abc_DEF-123/edit#gid=456":                                            "https://docs.google.com/spreadsheets/d/abc_DEF-123/export?format=csv&gid=456",
		"https://docs.google.com/spreadsheets/d/e/2PACX-xyz/pubhtml?gid=7":                                           "https://docs.google.com/spreadsheets/d/e/2PACX-xyz/pub?output=csv&gid=7",
		"1AbCdEfGhIjKlMnOpQrStUvWxYz0123456789abcdefg":                                                               "https://docs.google.com/spreadsheets/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456789abcdefg/export?format=csv",
		"https://example.com/hosts.csv":                                                                              "https://example.com/hosts.csv",
	}
	for in, want := range cases {
		got, err := ExportURL(in)
		if err != nil || got != want {
			t.Errorf("ExportURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "not a url", "https://docs.google.com/document/d/x/edit"} {
		if _, err := ExportURL(bad); err == nil {
			t.Errorf("ExportURL(%q) should fail", bad)
		}
	}
}

func TestParseHeaderTable(t *testing.T) {
	rows := [][]string{
		{"Lab hosts", "", ""},
		{"名稱", "IP 位址", "SSH Port", "帳號", "案場"},
		{"Router A", "10.0.0.1", "2222", "admin", "Site A"},
		{"Router B", "10.0.0.2", "", "", "Site A"},
		{"", "", "", "", ""},
		{"note: ask Bob for passwords", "", "", "", ""},
	}
	r := Parse(rows, Options{})
	if len(r.Sessions) != 2 {
		t.Fatalf("got %d sessions, skipped %v", len(r.Sessions), r.Skipped)
	}
	a, b := r.Sessions[0], r.Sessions[1]
	if a.Name != "Router A" || a.SSH.Host != "10.0.0.1" || a.SSH.Port != 2222 || a.SSH.User != "admin" || a.Folder != "Site A" {
		t.Errorf("row A: %+v %+v", a, a.SSH)
	}
	if b.SSH.Port != 22 {
		t.Errorf("row B default port: %d", b.SSH.Port)
	}
	if len(r.Folders) != 1 || r.Folders[0] != "Site A" {
		t.Errorf("folders %v", r.Folders)
	}
}

func TestParsePortsOnlyWithDefaultHost(t *testing.T) {
	rows := [][]string{
		{"姓名", "Port"},
		{"王小明", "20001"},
		{"陳大華", "20002"},
	}
	r := Parse(rows, Options{})
	if len(r.Sessions) != 0 || len(r.Skipped) != 2 {
		t.Fatalf("without default host: %d sessions, %d skipped", len(r.Sessions), len(r.Skipped))
	}
	r = Parse(rows, Options{DefaultHost: "gw.example.com", DefaultUser: "student"})
	if len(r.Sessions) != 2 {
		t.Fatalf("got %d sessions", len(r.Sessions))
	}
	s := r.Sessions[1]
	if s.Name != "陳大華" || s.SSH.Host != "gw.example.com" || s.SSH.Port != 20002 || s.SSH.User != "student" {
		t.Errorf("got %+v %+v", s, s.SSH)
	}
}

func TestParseFreeText(t *testing.T) {
	rows := [][]string{
		{"web", "ssh -p 2201 deploy@web.example.com"},
		{"db", "ssh db.example.com -p 2202"},
		{"cam", "root@192.168.1.50:8022"},
		{"switch", "192.168.1.1:22"},
		{"just a heading"},
	}
	r := Parse(rows, Options{})
	if len(r.Sessions) != 4 {
		t.Fatalf("got %d sessions: %v", len(r.Sessions), r.Skipped)
	}
	want := []struct {
		name, host, user string
		port             int
	}{
		{"web", "web.example.com", "deploy", 2201},
		{"db", "db.example.com", "", 2202},
		{"cam", "192.168.1.50", "root", 8022},
		{"switch", "192.168.1.1", "", 22},
	}
	for i, w := range want {
		s := r.Sessions[i]
		if s.Name != w.name || s.SSH.Host != w.host || s.SSH.User != w.user || s.SSH.Port != w.port {
			t.Errorf("%d: got %s %s %s %d", i, s.Name, s.SSH.Host, s.SSH.User, s.SSH.Port)
		}
	}
}

func TestDuplicateNames(t *testing.T) {
	r := Build([]Entry{{Name: "x", Host: "a.b", Port: 1}, {Name: "x", Host: "a.b", Port: 2}}, Options{})
	if r.Sessions[0].Name != "x" || r.Sessions[1].Name != "x (2)" {
		t.Errorf("names %q %q", r.Sessions[0].Name, r.Sessions[1].Name)
	}
}

func TestClassifyAvoidsFalseMatches(t *testing.T) {
	for _, h := range []string{"Description", "Report", "Support", "Tips"} {
		if k, _ := classify(h); k == colHost || k == colPort {
			t.Errorf("%q classified as host/port", h)
		}
	}
}

func TestSplitPorts(t *testing.T) {
	cases := map[string]string{
		"2222, 3333":                              "2222 3333",
		"7100/7200\n\n7720":                       "7100 7200 7720",
		"6010改6000":                               "6000",
		"改 8580 (原8460)":                          "8580",
		"6510 改 7530(2022換機器)":                    "7530",
		"7000,7010 \n(old:122.116.230.148:50222)": "7000 7010",
		"8010, 8000?":                             "8010 8000",
		"7900, 7910 \n4ch:7940 TR:7950":           "7900 7910 4ch:7940 TR:7950",
		"TR: 6810\nVS4: 6820":                     "TR:6810 VS4:6820",
		"6230 (i5 VM)\n6200, 6210\n6220":          "i5 VM:6230 6200 6210 6220",
		"7550 小電腦 \n 7560 pi2":                    "小電腦:7550 pi2:7560",
		"6150(跟鴻匯衝)":                              "6150",
		"8030 與金邊衝突 尚未修改":                         "8030",
		"ID:9130":                                 "",
		" ":                                       "",
		"7310,7320,7330":                          "7310 7320 7330",
	}
	for in, want := range cases {
		var got []string
		for _, it := range splitPorts(in) {
			if it.Label != "" {
				got = append(got, it.Label+":"+strconv.Itoa(it.Port))
			} else {
				got = append(got, strconv.Itoa(it.Port))
			}
		}
		if g := strings.Join(got, " "); g != want {
			t.Errorf("splitPorts(%q) = %q, want %q", in, g, want)
		}
	}
}

func TestSiteSheetLayout(t *testing.T) {
	// one row per site, no host column, several ports per cell, a "Server"
	// column holding notes, and a name column titled with a project name
	rows := [][]string{
		{"ACME Hospital", "WAN", "Server", "Model", "SSH port", "Web port"},
		{"Site A\nBuilding 1", "4G", "server: 192.168.68.1\nssh 6230 enp0s8: 10.0.3.15", "M1", "2222, 3333", "4444"},
		{"Site B", "", "", "", "6010改6000", "6011"},
		{"", "", "extra device", "", "7780", ""},
		{"Site C", "", "", "", "", ""},
	}
	r := Parse(rows, Options{DefaultHost: "relay.example.com"})
	var got []string
	for _, s := range r.Sessions {
		got = append(got, s.Name+"@"+s.SSH.Host+":"+strconv.Itoa(s.SSH.Port))
	}
	want := "Site A Building 1 (2222)@relay.example.com:2222|Site A Building 1 (3333)@relay.example.com:3333|Site B@relay.example.com:6000|Site B (7780)@relay.example.com:7780"
	if g := strings.Join(got, "|"); g != want {
		t.Errorf("got  %s\nwant %s", g, want)
	}
}
