package api

import (
	"errors"
	"testing"
)

func TestConnRegistryEnforcesPerUserCap(t *testing.T) {
	t.Parallel()

	reg := newConnRegistry(2, 100)

	r1, err := reg.acquire("user-1", "1.2.3.4")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if _, err := reg.acquire("user-1", "1.2.3.5"); err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	if _, err := reg.acquire("user-1", "1.2.3.6"); !errors.Is(err, errTooManyUserConns) {
		t.Fatalf("expected errTooManyUserConns, got %v", err)
	}

	// Releasing frees a slot.
	r1()

	if _, err := reg.acquire("user-1", "1.2.3.7"); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
}

func TestConnRegistryEnforcesPerIPCap(t *testing.T) {
	t.Parallel()

	reg := newConnRegistry(100, 2)

	if _, err := reg.acquire("user-1", "1.2.3.4"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if _, err := reg.acquire("user-2", "1.2.3.4"); err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	if _, err := reg.acquire("user-3", "1.2.3.4"); !errors.Is(err, errTooManyIPConns) {
		t.Fatalf("expected errTooManyIPConns, got %v", err)
	}
}

func TestConnRegistryDoesNotLeakUserSlotWhenIPCapRejects(t *testing.T) {
	t.Parallel()

	reg := newConnRegistry(1, 1)

	release, err := reg.acquire("user-1", "1.2.3.4")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// user-2 is rejected on the IP cap; that must not consume user-2's own
	// per-user budget.
	if _, err := reg.acquire("user-2", "1.2.3.4"); !errors.Is(err, errTooManyIPConns) {
		t.Fatalf("expected errTooManyIPConns, got %v", err)
	}

	release()

	if _, err := reg.acquire("user-2", "1.2.3.4"); err != nil {
		t.Fatalf("user-2 should have a free slot: %v", err)
	}
}

func TestConnRegistryDisabledWhenCapsAreZero(t *testing.T) {
	t.Parallel()

	reg := newConnRegistry(0, 0)

	for range 50 {
		if _, err := reg.acquire("user-1", "1.2.3.4"); err != nil {
			t.Fatalf("caps of zero must disable limiting, got %v", err)
		}
	}
}
