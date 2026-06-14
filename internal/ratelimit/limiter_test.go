package ratelimit

import (
	"testing"
	"time"
)

func TestIsRateLimitedAllowsUnderLimit(t *testing.T) {
	ClearParityWindows()
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < 5; i++ {
		if IsRateLimited("test-key", 5, 60000, now) {
			t.Fatalf("request %d should be allowed", i)
		}
	}
}

func TestIsRateLimitedBlocksOverLimit(t *testing.T) {
	ClearParityWindows()
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < 5; i++ {
		IsRateLimited("block-key", 5, 60000, now)
	}
	if !IsRateLimited("block-key", 5, 60000, now) {
		t.Fatal("expected block after limit")
	}
}

func TestIsRateLimitedSeparateBuckets(t *testing.T) {
	ClearParityWindows()
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < 3; i++ {
		IsRateLimited("key-a", 3, 60000, now)
	}
	if !IsRateLimited("key-a", 3, 60000, now) {
		t.Fatal("key-a should be blocked")
	}
	if IsRateLimited("key-b", 3, 60000, now) {
		t.Fatal("key-b should still be allowed")
	}
}

func TestLimiterAllow(t *testing.T) {
	l := NewLimiter()
	for i := 0; i < 3; i++ {
		if !l.Allow("k", 3, time.Minute) {
			t.Fatalf("expected allow at %d", i)
		}
	}
	if l.Allow("k", 3, time.Minute) {
		t.Fatal("expected block")
	}
}

func TestGatesSessionAndUpload(t *testing.T) {
	g := NewGates(Limits{
		SessionsTenant: 2,
		UploadTenant:   2,
	})
	if !g.AllowSessionCreate("tenant-a") {
		t.Fatal("first session create should pass")
	}
	if !g.AllowSessionCreate("tenant-a") {
		t.Fatal("second session create should pass")
	}
	if g.AllowSessionCreate("tenant-a") {
		t.Fatal("third session create should block")
	}
	if !g.AllowSessionCreate("tenant-b") {
		t.Fatal("other tenant should pass")
	}
	if !g.AllowUpload("tenant-a") {
		t.Fatal("first upload should pass")
	}
	if !g.AllowUpload("tenant-a") {
		t.Fatal("second upload should pass")
	}
	if g.AllowUpload("tenant-a") {
		t.Fatal("third upload should block")
	}
}
