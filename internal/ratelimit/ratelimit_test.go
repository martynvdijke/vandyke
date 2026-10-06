package ratelimit

import (
	"testing"
	"time"
)

func TestBurstThenDeny(t *testing.T) {
	l := New(60, 2)

	if !l.Allow("10.0.0.1") {
		t.Fatal("first call denied, want allowed")
	}
	if !l.Allow("10.0.0.1") {
		t.Fatal("second call denied, want allowed by burst")
	}
	if l.Allow("10.0.0.1") {
		t.Fatal("third call allowed, want denied after burst")
	}
	if got := l.RetryAfterSeconds(); got != 60 {
		t.Fatalf("RetryAfterSeconds = %d, want 60", got)
	}
	if got := l.Len(); got != 1 {
		t.Fatalf("tracked keys = %d, want 1", got)
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l := New(60, 1)

	if !l.Allow("a") {
		t.Fatal("first key denied")
	}
	if l.Allow("a") {
		t.Fatal("first key allowed twice, want denied")
	}
	if !l.Allow("b") {
		t.Fatal("second key denied, want allowed")
	}
	if got := l.Len(); got != 2 {
		t.Fatalf("tracked keys = %d, want 2", got)
	}
}

func TestIdleKeysAreSwept(t *testing.T) {
	base := time.Now()
	l := New(60, 5)
	l.now = func() time.Time { return base }

	if !l.Allow("stale") {
		t.Fatal("stale key denied")
	}

	base = base.Add(25 * time.Hour)
	if !l.Allow("fresh") {
		t.Fatal("fresh key denied")
	}
	if got := l.Len(); got != 1 {
		t.Fatalf("tracked keys after sweep = %d, want 1", got)
	}
}

func TestInvalidConfigurationIsClamped(t *testing.T) {
	l := New(0, 0)
	if !l.Allow("ip") {
		t.Fatal("first call denied, want allowed")
	}
	if l.Allow("ip") {
		t.Fatal("second call allowed, want denied (burst clamped to 1)")
	}
	if got := l.RetryAfterSeconds(); got != 3600 {
		t.Fatalf("RetryAfterSeconds = %d, want 3600", got)
	}
	if l.Allow("other-ip") != true {
		t.Fatal("other key denied, want allowed")
	}
}
