package application

import (
	"hash/fnv"
	"sync"
)

type Coordinator struct{ slots []sync.Mutex }

func NewCoordinator(size int) *Coordinator {
	if size < 1 {
		size = 64
	}
	return &Coordinator{slots: make([]sync.Mutex, size)}
}

func (c *Coordinator) lock(batchID string) func() {
	h := fnv.New32a()
	_, _ = h.Write([]byte(batchID))
	slot := &c.slots[int(h.Sum32())%len(c.slots)]
	slot.Lock()
	return slot.Unlock
}
