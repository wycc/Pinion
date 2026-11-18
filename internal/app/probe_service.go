package app

import (
	"github.com/ka1hung/pinion/internal/probe"
	"github.com/ka1hung/pinion/internal/store"
)

// ProbeService reports whether saved sessions are reachable. Every SSH,
// Telnet and TCP-client session is probed with a plain TCP connect (64 at a
// time, 2 s timeout) when the app starts, then every 5 minutes. Results are
// sent to the UI as "probe:results" events.
type ProbeService struct {
	c *Core
	p *probe.Prober
}

// NewProbeService creates the service and starts probing.
func NewProbeService(c *Core) *ProbeService {
	s := &ProbeService{c: c}
	s.p = &probe.Prober{
		Workers: 64,
		Targets: func() []probe.Target { return probeTargets(c.Store.List()) },
		Enabled: func() bool { return c.Store.Settings().SessionProbe },
		Publish: func(batch []probe.Result) { c.Emit("probe:results", batch) },
	}
	s.p.Start()
	c.OnShutdown(s.p.Stop)
	return s
}

// Results returns the latest probe result per session ID.
func (s *ProbeService) Results() map[string]probe.Result { return s.p.Results() }

// ProbeNow re-probes the given sessions right away (all when empty).
func (s *ProbeService) ProbeNow(ids []string) { s.p.ProbeNow(ids...) }

// probeTargets lists the endpoint each probeable session connects to first:
// its own host and port, or its first jump host for SSH through ProxyJump.
func probeTargets(list []*store.Session) []probe.Target {
	byID := make(map[string]*store.Session, len(list))
	for _, s := range list {
		byID[s.ID] = s
	}
	var out []probe.Target
	add := func(id, host string, port int) {
		if host != "" && port > 0 {
			out = append(out, probe.Target{ID: id, Host: host, Port: port})
		}
	}
	for _, s := range list {
		switch {
		case s.Type == store.TypeSSH && s.SSH != nil:
			if len(s.SSH.JumpHosts) > 0 {
				j := s.SSH.JumpHosts[0]
				if j.SessionID != "" {
					if ref := byID[j.SessionID]; ref != nil && ref.SSH != nil {
						add(s.ID, ref.SSH.Host, ref.SSH.Port)
					}
				} else {
					port := j.Port
					if port == 0 {
						port = 22
					}
					add(s.ID, j.Host, port)
				}
				continue
			}
			add(s.ID, s.SSH.Host, s.SSH.Port)
		case s.Type == store.TypeTelnet && s.Telnet != nil:
			add(s.ID, s.Telnet.Host, s.Telnet.Port)
		case s.Type == store.TypeTCP && s.Net != nil && s.Net.Mode == "client":
			add(s.ID, s.Net.Host, s.Net.Port)
		}
	}
	return out
}
