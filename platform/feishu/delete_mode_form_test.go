package feishu

import (
	"reflect"
	"testing"
)

func TestParseDeleteModeCheckerNameRejectsInvalidNames(t *testing.T) {
	if got, ok := parseDeleteModeCheckerName("other"); ok || got != "" {
		t.Fatalf("parseDeleteModeCheckerName(other) = %q, %v; want empty false", got, ok)
	}
	if got, ok := parseDeleteModeCheckerName(deleteModeCheckerNamePrefix); ok || got != "" {
		t.Fatalf("parseDeleteModeCheckerName(empty suffix) = %q, %v; want empty false", got, ok)
	}
	if got, ok := parseDeleteModeCheckerName(deleteModeCheckerNamePrefix + "not-hex"); ok || got != "" {
		t.Fatalf("parseDeleteModeCheckerName(invalid hex) = %q, %v; want empty false", got, ok)
	}
}

func TestCollectDeleteModeSelectedFromFormValue_ParsesTruthyValues(t *testing.T) {
	got := collectDeleteModeSelectedFromFormValue(map[string]any{
		deleteModeCheckerName("session-b"): true,
		deleteModeCheckerName("session-a"): " YES ",
		deleteModeCheckerName("session-c"): float64(1),
		deleteModeCheckerName("session-d"): int(2),
		deleteModeCheckerName("session-e"): int64(3),
		deleteModeCheckerName("session-f"): "off",
		deleteModeCheckerName("session-g"): float64(0),
		deleteModeCheckerName("session-h"): int(0),
		deleteModeCheckerName("session-i"): int64(0),
		"unrelated":                        true,
	})
	want := []string{"session-a", "session-b", "session-c", "session-d", "session-e"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected = %#v, want %#v", got, want)
	}
}

func TestIsTruthyFormValue(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want bool
	}{
		{"bool true", true, true},
		{"bool false", false, false},
		{"string true", "true", true},
		{"string one", "1", true},
		{"string yes", "yes", true},
		{"string on", "on", true},
		{"string false", "false", false},
		{"float nonzero", float64(-1), true},
		{"float zero", float64(0), false},
		{"int nonzero", int(7), true},
		{"int zero", int(0), false},
		{"int64 nonzero", int64(9), true},
		{"int64 zero", int64(0), false},
		{"nil", nil, false},
		{"slice", []string{"true"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTruthyFormValue(tt.in); got != tt.want {
				t.Fatalf("isTruthyFormValue(%#v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
