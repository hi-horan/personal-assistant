package app

import (
	"strings"
	"testing"
)

func TestValidateScope(t *testing.T) {
	if err := validateScope("app", "user"); err != nil {
		t.Fatalf("validateScope() error = %v", err)
	}
	if err := validateScope(strings.Repeat("a", 257), "user"); err == nil {
		t.Fatal("validateScope(long app_name) error = nil, want error")
	}
	if err := validateScope("app", strings.Repeat("u", 51)); err == nil {
		t.Fatal("validateScope(long user_id) error = nil, want error")
	}
}
