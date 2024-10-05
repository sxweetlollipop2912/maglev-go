package chash

import (
	"sort"
	"sync"
)

var (
	SmallSize = 65537
	LargeSize = 655373
)

// ConsistentHash is a consistent hash interface. Its implementation is thread-safe.
//
// In Maglev, this algorithm is used to map packets to backends. Depending on the hash of the packet header (the key),
// a backend is selected from the lookup table using MOD operation.
//
// This hash algorithm allows dynamic addition and removal of backends, with minimal disruption to the existing mapping.
// Particularly useful when packets should be routed to the same backend consistently to maintain connection state.
//
// Implementation defines in the Maglev paper:
// https://static.googleusercontent.com/media/research.google.com/en//pubs/archive/44824.pdf
type ConsistentHash interface {
	// Add adds the given backends to the consistent hash.
	Add(backends ...string)
	// Remove removes the given backends from the consistent hash.
	Remove(backends ...string)
	// Hash returns the backend for the given key.
	Hash(key uint64) string
	// Size returns the size of the lookup table.
	Size() uint32
}

type consistentHashImpl struct {
	size uint32

	backends map[string]struct {
		id     int
		offset uint32
		skip   uint32
	}
	backendsMtx sync.RWMutex

	lookup    []string
	lookupMtx sync.RWMutex
}

// NewConsistentHash creates a new ConsistentHash with the given size.
// The size must be a prime number. Use SmallSize or LargeSize for common sizes.
func NewConsistentHash(size uint32) ConsistentHash {
	return &consistentHashImpl{
		size: size,
		backends: make(map[string]struct {
			id     int
			offset uint32
			skip   uint32
		}),
	}
}

func (c *consistentHashImpl) Size() uint32 {
	return c.size
}

// Add runs in O(n log n) time.
func (c *consistentHashImpl) Add(backends ...string) {
	c.backendsMtx.Lock()

	for _, backend := range backends {
		c.backends[backend] = struct {
			id     int
			offset uint32
			skip   uint32
		}{
			id: len(c.backends),
			// Generate offset and skip using crc32 hash
			offset: crc32(append([]byte(backend), []byte("offset")...)) % c.Size(),
			skip:   crc32(append([]byte(backend), []byte("skip")...))%(c.Size()-1) + 1,
		}
	}

	c.backendsMtx.Unlock()
	c.backendsMtx.RLock()
	defer c.backendsMtx.RUnlock()
	c.lookupMtx.Lock()
	defer c.lookupMtx.Unlock()
	c.computeLookupTable()
}

// Remove runs in O(n log n) time.
func (c *consistentHashImpl) Remove(backends ...string) {
	c.backendsMtx.Lock()

	for _, backend := range backends {
		delete(c.backends, backend)
	}

	c.backendsMtx.Unlock()
	c.backendsMtx.RLock()
	defer c.backendsMtx.RUnlock()
	c.lookupMtx.Lock()
	defer c.lookupMtx.Unlock()
	c.computeLookupTable()
}

// Hash runs in O(1) amortized time,
// but if the lookup table is not initialized, it runs in O(n log n) time.
func (c *consistentHashImpl) Hash(key uint64) string {
	c.backendsMtx.RLock()
	defer c.backendsMtx.RUnlock()

	if len(c.backends) == 0 {
		return ""
	}

	c.lookupMtx.RLock()
	if len(c.lookup) != int(c.Size()) {
		c.lookupMtx.RUnlock()
		c.lookupMtx.Lock()
		defer c.lookupMtx.Unlock()
		c.computeLookupTable()
	} else {
		defer c.lookupMtx.RUnlock()
	}
	return c.lookup[key%uint64(c.Size())]
}

// computeLookupTable computes the lookup table for the consistent hash.
// Assumes backendsMtx is read-locked, and lookupMtx is write-locked.
// Runs in O(n log n) time.
func (c *consistentHashImpl) computeLookupTable() {
	// Re-initialize the lookup table
	c.lookup = make([]string, c.Size())

	// Initialize next array
	next := make([]uint32, len(c.backends))

	// Initialize entry array
	entry := make([]int, c.Size())
	for j := range entry {
		entry[j] = -1
	}

	// Start populating the lookup table
	backends := c.getBackendsAsSlice()
	var n uint32 = 0
	for {
		for i := 0; i < len(backends); i++ {
			// Get the next candidate from permutation
			candidate := c.permutationAt(backends[i], next[i])

			// Ensure the candidate is not already taken
			for entry[candidate] >= 0 {
				next[i]++
				candidate = c.permutationAt(backends[i], next[i])
			}

			// Assign the backend to the candidate position in the lookup table
			entry[candidate] = i
			c.lookup[candidate] = backends[i]
			next[i]++

			// Increment n and check if we've filled the lookup table
			n++
			if n == c.Size() {
				return
			}
		}
	}
}

// permutationAt returns the j-th permutation of the backend.
// Assumes backendsMtx is read-locked.
func (c *consistentHashImpl) permutationAt(name string, j uint32) uint32 {
	be := c.backends[name]
	return (be.offset + j*be.skip) % c.Size()
}

// getBackendsAsSlice returns the backends as a sorted slice.
// Assumes backendsMtx is read-locked.
// Runs in O(n log n) time.
func (c *consistentHashImpl) getBackendsAsSlice() []string {
	backends := make([]string, 0, len(c.backends))
	for backend := range c.backends {
		backends = append(backends, backend)
	}
	// sort by id
	sort.Slice(backends, func(i, j int) bool {
		return c.backends[backends[i]].id < c.backends[backends[j]].id
	})
	return backends
}
