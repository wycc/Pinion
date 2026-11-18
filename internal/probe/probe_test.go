package probe

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeNet struct {
	mu     sync.Mutex
	active int32
	peak   int32
	dials  map[string]int
	delay  time.Duration
}

func (f *fakeNet) dial(ctx context.Context, _, addr string) (net.Conn, error) {
	n := atomic.AddInt32(&f.active, 1)
	defer atomic.AddInt32(&f.active, -1)
	for {
		p := atomic.LoadInt32(&f.peak)
		if n <= p || atomic.CompareAndSwapInt32(&f.peak, p, n) {
			break
		}
	}
	f.mu.Lock()
	f.dials[addr]++
	f.mu.Unlock()
	switch {
	case strings.HasPrefix(addr, "slow"):
		<-ctx.Done()
		return nil, ctx.Err()
	case strings.HasPrefix(addr, "down"):
		return nil, errors.New("connection refused")
	}
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	a, b := net.Pipe()
	_ = b.Close()
	return a, nil
}

func (f *fakeNet) count(addr string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dials[addr]
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestInitialSweepUsesAllWorkers(t *testing.T) {
	f := &fakeNet{dials: map[string]int{}, delay: 100 * time.Millisecond}
	var targets []Target
	for i := 0; i < 200; i++ {
		targets = append(targets, Target{ID: string(rune('a'+i%26)) + string(rune('0'+i/26)), Host: "up", Port: 1000 + i})
	}
	var published int32
	p := &Prober{Workers: 64, Timeout: time.Second, Interval: time.Hour, Tick: 20 * time.Millisecond,
		Targets: func() []Target { return targets }, Dial: f.dial,
		Publish: func(b []Result) { atomic.AddInt32(&published, int32(len(b))) }}
	start := time.Now()
	p.Start()
	defer p.Stop()
	waitFor(t, func() bool { return len(p.Results()) == 200 })
	if el := time.Since(start); el > 1500*time.Millisecond {
		t.Errorf("sweep of 200 x 100ms with 64 workers took %v", el)
	}
	if pk := atomic.LoadInt32(&f.peak); pk != 64 {
		t.Errorf("peak concurrency %d, want 64", pk)
	}
	waitFor(t, func() bool { return atomic.LoadInt32(&published) == 200 })
	for _, r := range p.Results() {
		if !r.OK {
			t.Fatalf("result %+v", r)
		}
	}
}

func TestTimeoutRefusedAndRescheduling(t *testing.T) {
	f := &fakeNet{dials: map[string]int{}}
	var mu sync.Mutex
	targets := []Target{{ID: "u", Host: "up", Port: 22}, {ID: "s", Host: "slow", Port: 22}, {ID: "d", Host: "down", Port: 22}}
	p := &Prober{Workers: 4, Timeout: 150 * time.Millisecond, Interval: 300 * time.Millisecond, Tick: 20 * time.Millisecond,
		Targets: func() []Target { mu.Lock(); defer mu.Unlock(); return append([]Target(nil), targets...) }, Dial: f.dial}
	p.Start()
	defer p.Stop()
	waitFor(t, func() bool { return len(p.Results()) == 3 })
	r := p.Results()
	if !r["u"].OK || r["s"].OK || r["d"].OK {
		t.Fatalf("results %+v", r)
	}
	if !strings.Contains(r["s"].Error, "no response") || !strings.Contains(r["d"].Error, "refused") {
		t.Errorf("errors: slow=%q down=%q", r["s"].Error, r["d"].Error)
	}
	// re-probed after Interval, not before
	if n := f.count("up:22"); n != 1 {
		t.Errorf("probed %d times before interval", n)
	}
	waitFor(t, func() bool { return f.count("up:22") >= 2 })

	// an edited address is probed at once; a deleted session disappears
	mu.Lock()
	targets = []Target{{ID: "u", Host: "up", Port: 2222}}
	mu.Unlock()
	p.Refresh()
	waitFor(t, func() bool {
		return f.count("up:2222") == 1 && len(p.Results()) == 1 && p.Results()["u"].Addr == "up:2222"
	})

	// ProbeNow forces a probe before the interval
	before := f.count("up:2222")
	p.ProbeNow("u")
	waitFor(t, func() bool { return f.count("up:2222") > before })
}

func TestDisabled(t *testing.T) {
	f := &fakeNet{dials: map[string]int{}}
	var on atomic.Bool
	p := &Prober{Tick: 10 * time.Millisecond, Targets: func() []Target { return []Target{{ID: "x", Host: "up", Port: 1}} },
		Enabled: on.Load, Dial: f.dial}
	p.Start()
	defer p.Stop()
	time.Sleep(100 * time.Millisecond)
	if f.count("up:1") != 0 {
		t.Fatal("probed while disabled")
	}
	on.Store(true)
	waitFor(t, func() bool { return f.count("up:1") == 1 })
}

func TestShortErrorRealDial(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	addr := l.Addr().String()
	_ = l.Close() // nothing listens there now
	var d net.Dialer
	_, err = d.DialContext(context.Background(), "tcp", addr)
	if err == nil {
		t.Skip("port unexpectedly open")
	}
	if got := shortError(err); got != "connection refused" {
		t.Errorf("shortError = %q", got)
	}
}
