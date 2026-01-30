package printer_test

import (
	"testing"
	"time"

	"github.com/slok/sbx/internal/printer"
	"github.com/stretchr/testify/assert"
)

func TestTimeAgo(t *testing.T) {
	now := time.Now().UTC()

	tests := map[string]struct {
		time     time.Time
		expected string
	}{
		"1 second ago": {
			time:     now.Add(-1 * time.Second),
			expected: "1 second ago (UTC)",
		},
		"30 seconds ago": {
			time:     now.Add(-30 * time.Second),
			expected: "30 seconds ago (UTC)",
		},
		"1 minute ago": {
			time:     now.Add(-1 * time.Minute),
			expected: "1 minute ago (UTC)",
		},
		"45 minutes ago": {
			time:     now.Add(-45 * time.Minute),
			expected: "45 minutes ago (UTC)",
		},
		"1 hour ago": {
			time:     now.Add(-1 * time.Hour),
			expected: "1 hour ago (UTC)",
		},
		"5 hours ago": {
			time:     now.Add(-5 * time.Hour),
			expected: "5 hours ago (UTC)",
		},
		"1 day ago": {
			time:     now.Add(-24 * time.Hour),
			expected: "1 day ago (UTC)",
		},
		"7 days ago": {
			time:     now.Add(-7 * 24 * time.Hour),
			expected: "7 days ago (UTC)",
		},
		"future time": {
			time:     now.Add(5 * time.Minute),
			expected: "in the future (UTC)",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			result := printer.TimeAgo(test.time)
			assert.Equal(test.expected, result)
		})
	}
}

func TestFormatTimestamp(t *testing.T) {
	tests := map[string]struct {
		time     time.Time
		expected string
	}{
		"standard timestamp": {
			time:     time.Date(2026, 1, 30, 10, 15, 30, 0, time.UTC),
			expected: "2026-01-30 10:15:30 UTC",
		},
		"timestamp with different timezone gets converted to UTC": {
			time:     time.Date(2026, 1, 30, 10, 15, 30, 0, time.FixedZone("EST", -5*3600)),
			expected: "2026-01-30 15:15:30 UTC",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			result := printer.FormatTimestamp(test.time)
			assert.Equal(test.expected, result)
		})
	}
}
