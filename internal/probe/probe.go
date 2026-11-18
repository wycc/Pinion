// Package probe checks in the background whether saved sessions are
// reachable: a plain TCP connect to the session's host and port (the first
// jump host for SSH sessions that use one). Any accepted connection counts as
// reachable; no protocol is spoken. Every session is probed once when the
// prober starts, then again each Interval after its last probe.
package probe

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

// Target is one endpoint to probe.
type Target struct {
	ID   string // session ID
	Host string
	Port int
}

func (t Target) addr() string { return net.JoinHostPort(t.Host, strconv.Itoa(t.Port)) }

// Result is the outcome of the latest probe of a session.
type Result struct {
	ID        string `json:"id"`
	OK        bool   `json:"ok"`
	LatencyMs int    `json:"latencyMs"` // connect time when OK
	Error     string `json:"error,omitempty"`
	Addr      string `json:"addr"`
	At        int64  `json:"at"` // unix milliseconds
}

// Dialer opens the probe connection (net.Dialer.DialContext by default).
type Dialer func(ctx context.Context, network, addr string) (net.Conn, error)

// Prober schedules and runs probes.
type Prober struct {
	Workers  int           // concurrent probes (default 64)
	Timeout  time.Duration // per probe (default 2s)
	Interval time.Duration // between probes of one session (default 5m)
	Tick     time.Duration // how often due sessions are looked for (default 1s)

	Targets func() []Target      // current sessions to probe
	Enabled func() bool          // nil = always
	Publish func(batch []Result) // new results, batched
	Dial    Dialer

	mu       sync.Mutex
	state    map[string]*entry
	results  map[string]Result
	wake     chan struct{}
	cancel   context.CancelFunc
	done     chan struct{}
	finished chan Result
}

type entry struct {
	target   Target
	next     time.Time
	inFlight bool
}

func (p *Prober) defaults() {
	if p.Workers <= 0 {
		p.Workers = 64
	}
	if p.Timeout <= 0 {
		p.Timeout = 2 * time.Second
	}
	if p.Interval <= 0 {
		p.Interval = 5 * time.Minute
	}
	if p.Tick <= 0 {
		p.Tick = time.Second
	}
	if p.Dial == nil {
		var d net.Dialer
		p.Dial = d.DialContext
	}
}

// Start launches the scheduler and the worker pool.
func (p *Prober) Start() {
	p.defaults()
	p.state = map[string]*entry{}
	p.results = map[string]Result{}
	p.wake = make(chan struct{}, 1)
	p.finished = make(chan Result, p.Workers*2)
	p.done = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	jobs := make(chan Target, p.Workers)
	var wg sync.WaitGroup
	for i := 0; i < p.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range jobs {
				r := p.probe(ctx, t)
				select {
				case p.finished <- r:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go p.collect(ctx)
	go func() {
		defer close(p.done)
		defer func() { close(jobs); wg.Wait() }()
		tick := time.NewTicker(p.Tick)
		defer tick.Stop()
		for {
			p.schedule(ctx, jobs)
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			case <-p.wake:
			}
		}
	}()
}

// Stop ends all probing.
func (p *Prober) Stop() {
	if p.cancel != nil {
		p.cancel()
		<-p.done
	}
}

// Results returns the latest result of every probed session.
func (p *Prober) Results() map[string]Result {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]Result, len(p.results))
	for k, v := range p.results {
		out[k] = v
	}
	return out
}

// ProbeNow makes the given sessions due immediately (all when ids is empty).
func (p *Prober) ProbeNow(ids ...string) {
	p.mu.Lock()
	if len(ids) == 0 {
		for _, e := range p.state {
			e.next = time.Time{}
		}
	}
	for _, id := range ids {
		if e := p.state[id]; e != nil {
			e.next = time.Time{}
		}
	}
	p.mu.Unlock()
	p.poke()
}

// Refresh re-reads the targets now (e.g. after sessions were edited).
func (p *Prober) Refresh() { p.poke() }

func (p *Prober) poke() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// schedule syncs targets with the session list and queues due probes.
func (p *Prober) schedule(ctx context.Context, jobs chan<- Target) {
	if p.Enabled != nil && !p.Enabled() {
		return
	}
	targets := p.Targets()
	now := time.Now()
	var due []Target
	p.mu.Lock()
	seen := make(map[string]bool, len(targets))
	for _, t := range targets {
		seen[t.ID] = true
		e := p.state[t.ID]
		switch {
		case e == nil:
			e = &entry{target: t} // new session: probe now
			p.state[t.ID] = e
		case e.target != t:
			e.target, e.next = t, time.Time{} // address changed: probe now
			delete(p.results, t.ID)
		}
		if !e.inFlight && !now.Before(e.next) {
			e.inFlight = true
			due = append(due, t)
		}
	}
	for id := range p.state {
		if !seen[id] { // session deleted or no longer probeable
			delete(p.state, id)
			delete(p.results, id)
		}
	}
	p.mu.Unlock()
	for _, t := range due {
		select {
		case jobs <- t:
		case <-ctx.Done():
			return
		}
	}
}

func (p *Prober) probe(ctx context.Context, t Target) Result {
	r := Result{ID: t.ID, Addr: t.addr()}
	cctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	start := time.Now()
	c, err := p.Dial(cctx, "tcp", r.Addr)
	if err == nil {
		r.OK = true
		r.LatencyMs = int(time.Since(start).Milliseconds())
		_ = c.Close()
	} else if cctx.Err() == context.DeadlineExceeded {
		r.Error = "no response within " + p.Timeout.String()
	} else {
		r.Error = shortError(err)
	}
	r.At = time.Now().UnixMilli()
	return r
}

// collect records finished probes and publishes them in batches, so a sweep
// of hundreds of sessions becomes a few events instead of hundreds.
func (p *Prober) collect(ctx context.Context) {
	var batch []Result
	flush := time.NewTicker(300 * time.Millisecond)
	defer flush.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-p.finished:
			p.mu.Lock()
			e := p.state[r.ID]
			current := e != nil && e.target.addr() == r.Addr
			if e != nil {
				e.inFlight = false
				if current {
					e.next = time.Now().Add(p.Interval)
				}
			}
			if current {
				p.results[r.ID] = r
			}
			p.mu.Unlock()
			if current {
				batch = append(batch, r)
			}
		case <-flush.C:
			if len(batch) > 0 && p.Publish != nil {
				p.Publish(batch)
			}
			batch = nil
		}
	}
}

// shortError drops the "dial tcp host:port:" prefix; the address is shown
// separately.
func shortError(err error) string {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "cannot resolve host"
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		var sys *os.SyscallError
		if errors.As(op.Err, &sys) {
			return sys.Err.Error() // e.g. "connection refused"
		}
		return op.Err.Error()
	}
	return err.Error()
}
