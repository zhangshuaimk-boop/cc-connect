package codex

import (
	"testing"
	"time"
)

const codexTestPollInterval = 10 * time.Millisecond

func waitUntil(t *testing.T, timeout time.Duration, description string, condition func() bool, snapshot func() string) {
	t.Helper()
	if condition() {
		return
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(codexTestPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if condition() {
				return
			}
		case <-timer.C:
			if snapshot != nil {
				if detail := snapshot(); detail != "" {
					t.Fatalf("timed out waiting for %s: %s", description, detail)
				}
			}
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func waitForChannelClosed[T any](t *testing.T, ch <-chan T, timeout time.Duration, description string) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}
