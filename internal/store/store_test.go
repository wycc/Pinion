package store

import (
	"os"
	"path/filepath"
	"testing"
)

// Settings saved by an older version must get defaults for new fields.
func TestSettingsDefaultsForMissingFields(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"theme":"light"}`), 0o600)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := s.Settings()
	if st.Theme != "light" || !st.SmartCopyPaste || st.TermRightClick != "menu" || !st.SFTPAutoSwitch || !st.SFTPFollowTerminal {
		t.Fatalf("defaults not applied: %+v", st)
	}
}

func TestUpsertSSHUpdatesPortInPlace(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name string, port int, user string) *Session {
		return &Session{Name: name, Folder: "Lab", Type: TypeSSH, SSH: &SSHConfig{Host: "gw", Port: port, User: user}}
	}
	if a, u, err := s.UpsertSSH([]*Session{mk("pc1", 2001, "me"), mk("pc2", 2002, "")}); err != nil || a != 2 || u != 0 {
		t.Fatalf("first import: added %d updated %d err %v", a, u, err)
	}
	id := s.List()[0].ID
	a, u, err := s.UpsertSSH([]*Session{mk("pc1", 3001, ""), mk("pc2", 2002, ""), mk("pc3", 2003, "")})
	if err != nil || a != 1 || u != 1 {
		t.Fatalf("re-import: added %d updated %d err %v", a, u, err)
	}
	got, _ := s.Get(id)
	if got.SSH.Port != 3001 || got.SSH.User != "me" {
		t.Errorf("pc1 after re-import: port %d user %q", got.SSH.Port, got.SSH.User)
	}
	if n := len(s.List()); n != 3 {
		t.Errorf("want 3 sessions, got %d", n)
	}
}
