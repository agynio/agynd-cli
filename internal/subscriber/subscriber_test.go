package subscriber

import (
	"testing"
	"time"
)

func TestNextBackoffProgression(t *testing.T) {
	tests := []struct {
		name     string
		current  time.Duration
		expected time.Duration
	}{
		{name: "zero", current: 0, expected: time.Second},
		{name: "one-second", current: time.Second, expected: 2 * time.Second},
		{name: "two-seconds", current: 2 * time.Second, expected: 4 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := nextBackoff(test.current)
			if got != test.expected {
				t.Fatalf("expected %s, got %s", test.expected, got)
			}
		})
	}
}

func TestNextBackoffCapsAtThirtySeconds(t *testing.T) {
	if got := nextBackoff(20 * time.Second); got != 30*time.Second {
		t.Fatalf("expected cap at 30s, got %s", got)
	}
	if got := nextBackoff(30 * time.Second); got != 30*time.Second {
		t.Fatalf("expected cap at 30s, got %s", got)
	}
}
