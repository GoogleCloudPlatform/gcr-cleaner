// Copyright 2019 The GCR Cleaner Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gcrcleaner

import (
	"sync"
	"time"
)

// Cache is an interface used by the PubSub() function to prevent duplicate
// messages from being processed.
type Cache interface {
	// Insert inserts the item into the cache. If the item already exists, this
	// method returns true.
	Insert(string) bool

	// Stop stops the cache. When Stop returns, the cache must not perform any
	// additionally processing.
	Stop()
}

// timerCache is a Cache implementation that caches items for a configurable
// period of time.
type timerCache struct {
	lock     sync.RWMutex
	data     map[string]struct{}
	lifetime time.Duration

	stopCh  chan struct{}
	stopped bool
}

// NewTimerCache creates a new timer-based cache.
func NewTimerCache(lifetime time.Duration) *timerCache {
	return &timerCache{
		data:     make(map[string]struct{}),
		lifetime: lifetime,
		stopCh:   make(chan struct{}),
	}
}

// Stop stops the cache.
func (c *timerCache) Stop() {
	c.lock.Lock()
	if !c.stopped {
		close(c.stopCh)
		c.stopped = true
	}
	c.lock.Unlock()
}

// Insert adds the item to the cache. If the item already existed in the cache,
// this function returns false.
func (c *timerCache) Insert(s string) bool {
	// Read only
	c.lock.RLock()
	if _, ok := c.data[s]; ok {
		c.lock.RUnlock()
		return true
	}
	c.lock.RUnlock()

	// Full insert
	c.lock.Lock()
	if _, ok := c.data[s]; ok {
		c.lock.Unlock()
		return true
	}

	c.data[s] = struct{}{}
	c.lock.Unlock()

	// Start a timeout to delete the item from the cache.
	go c.timeout(s)

	return false
}

func (c *timerCache) timeout(s string) {
	select {
	case <-time.After(c.lifetime):
		c.lock.Lock()
		delete(c.data, s)
		c.lock.Unlock()
	case <-c.stopCh:
	}
}
