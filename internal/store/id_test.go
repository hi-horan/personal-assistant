package store

import (
	"sync"
	"testing"
	"time"
)

func TestMicrosecondIDAllocatorConcurrent(t *testing.T) {
	allocator := NewMicrosecondIDAllocator()
	const workers = 16
	const perWorker = 200

	ids := make(chan int64, workers*perWorker)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perWorker {
				ids <- allocator.NextID()
			}
		}()
	}
	wg.Wait()
	close(ids)

	seen := map[int64]bool{}
	min := time.Now().Add(-1 * time.Minute).UnixMicro()
	for id := range ids {
		if id < min {
			t.Fatalf("id = %d, want microsecond timestamp-like value >= %d", id, min)
		}
		if seen[id] {
			t.Fatalf("duplicate id %d", id)
		}
		seen[id] = true
	}
	if len(seen) != workers*perWorker {
		t.Fatalf("generated %d ids, want %d", len(seen), workers*perWorker)
	}
}

func TestParseID(t *testing.T) {
	if got, err := parseID("1700000000000000", "id"); err != nil || got != 1700000000000000 {
		t.Fatalf("parseID() = %d, %v", got, err)
	}
	for _, value := range []string{"", "abc", "-1", "0"} {
		if _, err := parseID(value, "id"); err == nil {
			t.Fatalf("parseID(%q) error = nil, want error", value)
		}
	}
}
