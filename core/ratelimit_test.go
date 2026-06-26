package core

import (
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_AllowWithinLimit(t *testing.T) {
	rl := NewRateLimiter(5, time.Minute)
	for i := 0; i < 5; i++ {
		if !rl.Allow("user1") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
}

func TestRateLimiter_BlockExceedingLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		rl.Allow("user1")
	}
	if rl.Allow("user1") {
		t.Error("4th request should be blocked")
	}
}

func TestRateLimiter_DifferentKeys(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)
	rl.Allow("user1")
	rl.Allow("user1")

	if rl.Allow("user1") {
		t.Error("user1 should be blocked")
	}
	if !rl.Allow("user2") {
		t.Error("user2 should be allowed (independent bucket)")
	}
}

func TestRateLimiter_WindowExpiry(t *testing.T) {
	rl := NewRateLimiter(2, 50*time.Millisecond)
	rl.Allow("user1")
	rl.Allow("user1")

	if rl.Allow("user1") {
		t.Error("should be blocked immediately")
	}

	time.Sleep(60 * time.Millisecond)

	if !rl.Allow("user1") {
		t.Error("should be allowed after window expires")
	}
}

func TestRateLimiter_Disabled(t *testing.T) {
	rl := NewRateLimiter(0, time.Minute)
	for i := 0; i < 100; i++ {
		if !rl.Allow("user1") {
			t.Error("should always allow when disabled")
		}
	}
}

func TestRateLimiter_Concurrent(t *testing.T) {
	rl := NewRateLimiter(100, time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rl.Allow("user1")
		}()
	}
	wg.Wait()
}

func TestRateLimiter_Stop(t *testing.T) {
	rl := NewRateLimiter(5, time.Minute)
	rl.Allow("user1")

	// Stop should not panic and should be idempotent
	rl.Stop()
	rl.Stop() // second call should be safe

	// Allow should still work after Stop (just no background cleanup)
	if !rl.Allow("user2") {
		t.Error("Allow should still work after Stop")
	}
}

func TestRateLimiter_StopDisabled(t *testing.T) {
	// A disabled limiter (maxMessages=0) should also handle Stop gracefully
	rl := NewRateLimiter(0, time.Minute)
	rl.Stop()
}

func TestRateLimiter_BurstExactlyAtLimit(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	defer rl.Stop()

	if !rl.Allow("user1") {
		t.Fatal("first request should be allowed")
	}
	if rl.Allow("user1") {
		t.Fatal("second request in same window should be blocked")
	}
	if !rl.Allow("user2") {
		t.Fatal("different key should get its own burst allowance")
	}
}

func TestRateLimiter_ResetAfterWindow(t *testing.T) {
	rl := NewRateLimiter(1, time.Nanosecond)
	defer rl.Stop()

	if !rl.Allow("user1") {
		t.Fatal("first request should be allowed")
	}
	if !rl.Allow("user1") {
		t.Fatal("zero millisecond window should reset on each call")
	}
}

func TestRateLimiter_NegativeConfigDisablesLimit(t *testing.T) {
	rl := NewRateLimiter(-1, time.Minute)
	defer rl.Stop()

	for i := 0; i < 10; i++ {
		if !rl.Allow("user1") {
			t.Fatalf("request %d should be allowed when maxMessages is negative", i+1)
		}
	}
}

func TestRateLimiter_ZeroWindowOnlyAllowsFirstBurst(t *testing.T) {
	rl := NewRateLimiter(2, 0)
	defer rl.Stop()

	if !rl.Allow("user1") {
		t.Fatal("first request should be allowed")
	}
	if !rl.Allow("user1") {
		t.Fatal("second request should be allowed at burst limit")
	}
	if !rl.Allow("user1") {
		t.Fatal("zero window should expire previous timestamps before enforcing the next burst")
	}
}

func TestRateLimiter_ConcurrentSameKeyHonorsLimit(t *testing.T) {
	rl := NewRateLimiter(25, time.Minute)
	defer rl.Stop()

	const workers = 100
	var wg sync.WaitGroup
	results := make(chan bool, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- rl.Allow("user1")
		}()
	}
	wg.Wait()
	close(results)

	allowed := 0
	for ok := range results {
		if ok {
			allowed++
		}
	}
	if allowed != 25 {
		t.Fatalf("allowed = %d, want exactly 25", allowed)
	}
}
