package messaging

import (
	"testing"
	"time"

	"github.com/fastclaw-ai/weclaw/config"
)

func TestParseActiveHours(t *testing.T) {
	tests := []struct {
		input string
		start int
		end   int
		err   bool
	}{
		{"09:00-18:00", 540, 1080, false},
		{"00:00-23:59", 0, 1439, false},
		{"22:00-06:00", 1320, 360, false},
		{"bad", 0, 0, true},
		{"25:00-18:00", 0, 0, true},
		{"09:00-18:60", 0, 0, true},
	}
	for _, tt := range tests {
		start, end, err := parseActiveHours(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("parseActiveHours(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseActiveHours(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if start != tt.start || end != tt.end {
			t.Errorf("parseActiveHours(%q) = (%d,%d), want (%d,%d)", tt.input, start, end, tt.start, tt.end)
		}
	}
}

func TestIsEffectivelyEmpty(t *testing.T) {
	tests := []struct {
		content string
		empty   bool
	}{
		{"", true},
		{"\n\n", true},
		{"# Title\n\n", true},
		{"# Title\n<!-- comment -->\n", true},
		{"check emails", false},
		{"# Title\n- do stuff\n", false},
	}
	for _, tt := range tests {
		got := isEffectivelyEmpty(tt.content)
		if got != tt.empty {
			t.Errorf("isEffectivelyEmpty(%q) = %v, want %v", tt.content, got, tt.empty)
		}
	}
}

func TestNewHeartbeatRunner_RejectsNonPositiveInterval(t *testing.T) {
	tests := []struct {
		interval string
		wantErr  bool
	}{
		{"0s", true},
		{"-1m", true},
		{"30m", false},
		{"1h", false},
	}
	for _, tt := range tests {
		cfg := config.HeartbeatConfig{Enabled: true, Interval: tt.interval}
		_, err := NewHeartbeatRunner(cfg, nil, "", nil)
		if tt.wantErr && err == nil {
			t.Errorf("interval %q: expected error", tt.interval)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("interval %q: unexpected error: %v", tt.interval, err)
		}
	}
}

func TestHeartbeatRunnerIsDuplicate(t *testing.T) {
	r := &HeartbeatRunner{recentHash: make(map[string]time.Time)}
	if r.isDuplicate("hello") {
		t.Error("first call should not be duplicate")
	}
	if !r.isDuplicate("hello") {
		t.Error("second call should be duplicate")
	}
	if r.isDuplicate("different") {
		t.Error("different content should not be duplicate")
	}
}

func TestParseTimeOfDay(t *testing.T) {
	tests := []struct {
		input string
		mins  int
		err   bool
	}{
		{"09:00", 540, false},
		{"00:00", 0, false},
		{"23:59", 1439, false},
		{"12:30", 750, false},
		{"25:00", 0, true},
		{"09:60", 0, true},
		{"bad", 0, true},
	}
	for _, tt := range tests {
		mins, err := parseTimeOfDay(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("parseTimeOfDay(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseTimeOfDay(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if mins != tt.mins {
			t.Errorf("parseTimeOfDay(%q) = %d, want %d", tt.input, mins, tt.mins)
		}
	}
}
