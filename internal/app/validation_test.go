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

func TestValidateTitle(t *testing.T) {
	if err := validateTitle(strings.Repeat("t", 100)); err != nil {
		t.Fatalf("validateTitle(100) error = %v", err)
	}
	if err := validateTitle(strings.Repeat("t", 101)); err == nil {
		t.Fatal("validateTitle(101) error = nil, want error")
	}
}

func TestValidateKind(t *testing.T) {
	if err := validateKind(strings.Repeat("k", 20)); err != nil {
		t.Fatalf("validateKind(20) error = %v", err)
	}
	if err := validateKind(strings.Repeat("k", 21)); err == nil {
		t.Fatal("validateKind(21) error = nil, want error")
	}
}
