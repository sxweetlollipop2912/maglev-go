package chash

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestConsistentHash(t *testing.T) {
	tests := []struct {
		name                string
		initialSize         uint32
		backendsToAdd       []string
		additionalBackends  []string
		backendsToRemove    []string
		keys                []uint64
		expectedHashesStep1 []string
		expectedHashesStep2 []string
		expectedHashesStep3 []string
	}{
		{
			name:                "Single backend",
			initialSize:         65537,
			backendsToAdd:       []string{"backend1"},
			keys:                []uint64{1, 2, 3},
			expectedHashesStep1: []string{"backend1", "backend1", "backend1"},
			expectedHashesStep2: nil, // No second step
			expectedHashesStep3: nil, // No backend removal
		},
		{
			name:                "Multiple backends",
			initialSize:         65537,
			backendsToAdd:       []string{"backend1", "backend2", "backend3"},
			keys:                []uint64{0, 1, 18},
			expectedHashesStep1: []string{"backend1", "backend3", "backend2"},
			expectedHashesStep2: nil, // No second step
			expectedHashesStep3: nil, // No backend removal
		},
		{
			name:                "Remove backend",
			initialSize:         65537,
			backendsToAdd:       []string{"backend1", "backend2", "backend3"},
			backendsToRemove:    []string{"backend2"},
			keys:                []uint64{0, 1, 18},
			expectedHashesStep1: []string{"backend1", "backend3", "backend2"},
			expectedHashesStep2: nil,                                          // No second step
			expectedHashesStep3: []string{"backend1", "backend3", "backend3"}, // After backend2 is removed
		},
		{
			name:                "Rehash after adding more backends",
			initialSize:         65537,
			backendsToAdd:       []string{"backend1", "backend2"},
			additionalBackends:  []string{"backend3", "backend4"},
			keys:                []uint64{0, 1, 18, 21},
			expectedHashesStep1: []string{"backend1", "backend2", "backend2", "backend1"},
			expectedHashesStep2: []string{"backend4", "backend3", "backend2", "backend1"}, // After adding backend3 and backend4
			expectedHashesStep3: nil,                                                      // No backend removal
		},
		{
			name:                "Add and Remove backends",
			initialSize:         65537,
			backendsToAdd:       []string{"backend1", "backend2"},
			additionalBackends:  []string{"backend3"},
			backendsToRemove:    []string{"backend1"},
			keys:                []uint64{0, 1, 18, 21},
			expectedHashesStep1: []string{"backend1", "backend2", "backend2", "backend1"},
			expectedHashesStep2: []string{"backend1", "backend3", "backend2", "backend1"}, // After adding backend3
			expectedHashesStep3: []string{"backend2", "backend3", "backend2", "backend3"}, // After removing backend1
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Create a new consistent hash with the given size
			ch := NewConsistentHash(test.initialSize)

			// Add initial backends
			if len(test.backendsToAdd) > 0 {
				ch.Add(test.backendsToAdd...)
			}

			// Verify hashes after initial addition of backends
			for i, key := range test.keys {
				assert.Equal(t, test.expectedHashesStep1[i], ch.Hash(key), "Hash mismatch for key %d in step 1", key)
			}

			// Add additional backends if necessary
			if test.additionalBackends != nil {
				ch.Add(test.additionalBackends...)

				// Verify hashes after adding additional backends
				for i, key := range test.keys {
					assert.Equal(t, test.expectedHashesStep2[i], ch.Hash(key), "Hash mismatch for key %d in step 2", key)
				}
			}

			// Remove backends if necessary
			if len(test.backendsToRemove) > 0 {
				ch.Remove(test.backendsToRemove...)

				// Verify hashes after removing backends
				for i, key := range test.keys {
					assert.Equal(t, test.expectedHashesStep3[i], ch.Hash(key), "Hash mismatch for key %d in step 3", key)
				}
			}
		})
	}
}
