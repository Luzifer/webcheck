package main

import (
	"sync"
	"time"
)

type ringDuration struct {
	store   []time.Duration
	current int

	lock sync.RWMutex
}

func newRingDuration(maxLen int) *ringDuration {
	return &ringDuration{
		store:   make([]time.Duration, 0, maxLen),
		current: -1, // Initial value to start the ring with element 0
	}
}

func (r *ringDuration) SetNext(i time.Duration) {
	r.lock.Lock()
	defer r.lock.Unlock()

	next := r.current + 1
	if next == cap(r.store) {
		next = 0
	}

	if next == len(r.store) {
		r.store = append(r.store, 0)
	}

	r.store[next] = i
	r.current = next
}

func (r *ringDuration) GetAll() []time.Duration {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return r.store
}

func (r *ringDuration) GetCurrent() time.Duration {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return r.store[r.current]
}
