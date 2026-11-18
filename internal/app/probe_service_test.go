package app

import (
	"strconv"
	"testing"

	"github.com/ka1hung/pinion/internal/store"
)

func TestProbeTargets(t *testing.T) {
	list := []*store.Session{
		{ID: "gw", Type: store.TypeSSH, SSH: &store.SSHConfig{Host: "gw.example", Port: 2200}},
		{ID: "a", Type: store.TypeSSH, SSH: &store.SSHConfig{Host: "a.example", Port: 22}},
		{ID: "viaRef", Type: store.TypeSSH, SSH: &store.SSHConfig{Host: "inner", Port: 22, JumpHosts: []store.JumpHost{{SessionID: "gw"}}}},
		{ID: "viaInline", Type: store.TypeSSH, SSH: &store.SSHConfig{Host: "inner", Port: 22, JumpHosts: []store.JumpHost{{Host: "j.example"}}}},
		{ID: "tel", Type: store.TypeTelnet, Telnet: &store.TelnetConfig{Host: "sw", Port: 23}},
		{ID: "tcp", Type: store.TypeTCP, Net: &store.NetConfig{Mode: "client", Host: "dev", Port: 502}},
		{ID: "srv", Type: store.TypeTCP, Net: &store.NetConfig{Mode: "server", Host: "0.0.0.0", Port: 9000}},
		{ID: "ser", Type: store.TypeSerial, Serial: &store.SerialConfig{Port: "COM3"}},
		{ID: "empty", Type: store.TypeSSH, SSH: &store.SSHConfig{Port: 22}},
	}
	got := map[string]string{}
	for _, tg := range probeTargets(list) {
		got[tg.ID] = tg.Host + ":" + strconv.Itoa(tg.Port)
	}
	want := map[string]string{"gw": "gw.example:2200", "a": "a.example:22", "viaRef": "gw.example:2200", "viaInline": "j.example:22", "tel": "sw:23", "tcp": "dev:502"}
	if len(got) != len(want) {
		t.Errorf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q", k, got[k], v)
		}
	}
}

