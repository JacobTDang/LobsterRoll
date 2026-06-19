// Package dispatch is a bounded worker pool that decouples slow work (Telegram
// sends) from the NATS callback goroutine: a saturated queue drops explicitly
// (via onDrop) instead of letting NATS silently drop messages, and shutdown
// drains the queue rather than abandoning buffered work.
package dispatch

import "sync"

// Pool runs submitted funcs on a fixed set of workers.
type Pool struct {
	jobs   chan func()
	wg     sync.WaitGroup
	mu     sync.Mutex
	closed bool
	onDrop func()
}

// New starts workers draining a queue of the given size. onDrop (may be nil) is
// called once per dropped job (queue full, or submit after Drain).
func New(workers, queue int, onDrop func()) *Pool {
	if workers < 1 {
		workers = 1
	}
	p := &Pool{jobs: make(chan func(), queue), onDrop: onDrop}
	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for f := range p.jobs { // drains buffered jobs after Close, then exits
				f()
			}
		}()
	}
	return p
}

// Submit enqueues f, or drops it (calling onDrop) if the queue is full or the
// pool is draining. Never blocks the caller and never sends on a closed channel.
func (p *Pool) Submit(f func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		p.drop()
		return
	}
	select {
	case p.jobs <- f:
	default:
		p.drop()
	}
}

func (p *Pool) drop() {
	if p.onDrop != nil {
		p.onDrop()
	}
}

// Drain stops accepting new work and blocks until every already-queued job has
// run. Idempotent.
func (p *Pool) Drain() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.jobs)
	p.mu.Unlock()
	p.wg.Wait()
}
