package store

import (
	"fmt"
	"strconv"
	"sync/atomic"
	"time"
)

type IDAllocator interface {
	NextID() int64
}

type MicrosecondIDAllocator struct {
	last atomic.Int64
}

func NewMicrosecondIDAllocator() *MicrosecondIDAllocator {
	return &MicrosecondIDAllocator{}
}

func (a *MicrosecondIDAllocator) NextID() int64 {
	for {
		now := time.Now().UnixMicro()
		last := a.last.Load()
		next := now
		if next <= last {
			next = last + 1
		}
		if a.last.CompareAndSwap(last, next) {
			return next
		}
	}
}

func formatID(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

func parseID(value, field string) (int64, error) {
	if value == "" {
		return 0, fmt.Errorf("%s is required", field)
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer timestamp id", field)
	}
	return id, nil
}

func parseOptionalID(value, field string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	return parseID(value, field)
}

func (s *Store) invocationID(raw string) int64 {
	if raw == "" {
		return s.ids.NextID()
	}
	if id, err := parseID(raw, "invocation_id"); err == nil {
		return id
	}
	id := s.ids.NextID()
	actual, _ := s.invocations.LoadOrStore(raw, id)
	return actual.(int64)
}
