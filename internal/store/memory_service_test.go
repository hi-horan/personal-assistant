package store

import (
	"os"
	"strings"
	"testing"
)

func TestSaveMemoryUsesBigintNullSentinel(t *testing.T) {
	source, err := os.ReadFile("memory_service.go")
	if err != nil {
		t.Fatalf("read memory_service.go: %v", err)
	}
	if strings.Contains(string(source), "NULLIF($8, 0)") || strings.Contains(string(source), "NULLIF($9, 0)") {
		t.Fatal("SaveMemory must cast NULLIF zero sentinels to bigint")
	}
	if !strings.Contains(string(source), "NULLIF($8, 0::bigint)") || !strings.Contains(string(source), "NULLIF($9, 0::bigint)") {
		t.Fatal("SaveMemory missing bigint casts for nullable source IDs")
	}
}
