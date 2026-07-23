package ratelimit

import (
	"testing"
	"time"
)

func TestBucketAllowsBurstThenDenies(t *testing.T) {
	start := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	b := NewBucket(3, 10, start)

	for i := range 3 {
		if !b.Allow(start) {
			t.Fatalf("call %d: expected allowed within burst", i+1)
		}
	}

	if b.Allow(start) {
		t.Fatal("expected the 4th call at t=0 to be denied")
	}
}

func TestBucketRefillsAtTheConfiguredRate(t *testing.T) {
	start := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	b := NewBucket(2, 10, start)

	b.Allow(start)
	b.Allow(start)

	if b.Allow(start) {
		t.Fatal("expected the bucket to be empty")
	}

	// 10 tokens/sec means one token every 100ms.
	if !b.Allow(start.Add(100 * time.Millisecond)) {
		t.Fatal("expected one token to have refilled after 100ms")
	}
}

func TestBucketCapsRefillAtCapacity(t *testing.T) {
	start := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	b := NewBucket(2, 10, start)

	later := start.Add(1 * time.Hour)

	if !b.Allow(later) || !b.Allow(later) {
		t.Fatal("expected a full bucket after idling")
	}

	if b.Allow(later) {
		t.Fatal("expected refill to be capped at capacity")
	}
}
