// Package workqueue runs tasks per instance on a few workers, keeping tasks
// with the same key (e.g. a chat) in order without blocking the caller.
package workqueue

import (
	"hash/fnv"
	"sync"
)

const queueSize = 1000

type Queue struct {
	workers int
	onPanic func(instanceID string, r interface{})
	onFull  func(instanceID string)

	mu    sync.Mutex
	pools map[string][]chan func()
}

// New creates a queue with `workers` goroutines per instance (started lazily).
// onPanic and onFull are optional hooks used for logging.
func New(workers int, onPanic func(instanceID string, r interface{}), onFull func(instanceID string)) *Queue {
	if workers < 1 {
		workers = 1
	}
	return &Queue{workers: workers, onPanic: onPanic, onFull: onFull, pools: make(map[string][]chan func())}
}

// Enqueue runs fn on the instance worker chosen by key. When that worker's
// queue is full, fn runs in its own goroutine (out of order) instead of blocking.
func (q *Queue) Enqueue(instanceID, key string, fn func()) {
	q.mu.Lock()
	queues, ok := q.pools[instanceID]
	if !ok {
		queues = make([]chan func(), q.workers)
		for i := range queues {
			queues[i] = make(chan func(), queueSize)
			go q.worker(instanceID, queues[i])
		}
		q.pools[instanceID] = queues
	}
	q.mu.Unlock()

	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	ch := queues[h.Sum32()%uint32(len(queues))]

	select {
	case ch <- fn:
	default:
		if q.onFull != nil {
			q.onFull(instanceID)
		}
		go q.run(instanceID, fn)
	}
}

func (q *Queue) worker(instanceID string, ch chan func()) {
	for fn := range ch {
		q.run(instanceID, fn)
	}
}

func (q *Queue) run(instanceID string, fn func()) {
	defer func() {
		if r := recover(); r != nil && q.onPanic != nil {
			q.onPanic(instanceID, r)
		}
	}()
	fn()
}
