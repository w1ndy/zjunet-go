package daemon

import (
	"testing"
	"time"
)

func TestFormatUptime(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		connectedAt time.Time
		want        string
	}{
		{
			name:        "unknown",
			connectedAt: time.Time{},
			want:        "unknown",
		},
		{
			name:        "truncates subsecond precision",
			connectedAt: now.Add(-(90*time.Second + 250*time.Millisecond)),
			want:        "1m30s",
		},
		{
			name:        "future timestamp",
			connectedAt: now.Add(time.Second),
			want:        "0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatUptime(tt.connectedAt, now); got != tt.want {
				t.Fatalf("formatUptime() = %q, want %q", got, tt.want)
			}
		})
	}
}
