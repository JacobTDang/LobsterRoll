package dispatch

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPool_RunsSubmittedWork(t *testing.T) {
	var n int32
	p := New(4, 64, nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		p.Submit(func() { atomic.AddInt32(&n, 1); wg.Done() })
	}
	wg.Wait()
	p.Drain()
	if atomic.LoadInt32(&n) != 50 {
		t.Fatalf("ran %d jobs, want 50", n)
	}
}

func TestPool_DrainDeliversBufferedJobs(t *testing.T) {
	// One worker, blocked on the first job, so the rest sit buffered. Drain must
	// finish them all rather than abandon them (the shutdown-loss regression).
	var ran int32
	release := make(chan struct{})
	p := New(1, 64, nil)
	p.Submit(func() { <-release; atomic.AddInt32(&ran, 1) }) // blocks the worker
	for i := 0; i < 9; i++ {
		p.Submit(func() { atomic.AddInt32(&ran, 1) }) // buffered behind it
	}
	close(release)
	p.Drain() // must block until all 10 have run
	if got := atomic.LoadInt32(&ran); got != 10 {
		t.Fatalf("ran %d, want 10 (Drain must finish buffered jobs)", got)
	}
}

func TestPool_SubmitAfterDrainDropsNoPanic(t *testing.T) {
	var drops int32
	p := New(2, 8, func() { atomic.AddInt32(&drops, 1) })
	p.Drain()
	p.Submit(func() { t.Error("job ran after Drain") }) // must drop, must not panic
	if atomic.LoadInt32(&drops) != 1 {
		t.Fatalf("drops = %d, want 1 (submit after drain drops)", drops)
	}
}

func TestPool_FullQueueDrops(t *testing.T) {
	var drops int32
	block := make(chan struct{})
	p := New(1, 1, func() { atomic.AddInt32(&drops, 1) })
	defer close(block)
	p.Submit(func() { <-block }) // occupies the single worker
	time.Sleep(10 * time.Millisecond)
	p.Submit(func() {}) // fills the 1-slot queue
	p.Submit(func() {}) // queue full -> dropped
	p.Submit(func() {}) // dropped
	if got := atomic.LoadInt32(&drops); got < 1 {
		t.Fatalf("drops = %d, want >=1 (full queue must drop)", got)
	}
}

func TestPool_DrainIdempotent(t *testing.T) {
	p := New(2, 4, nil)
	p.Drain()
	p.Drain() // must not panic (double close)
}
