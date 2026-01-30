package printer

import (
	"fmt"
	"time"
)

// TimeAgo returns a human-readable relative time string in UTC.
// Examples: "5 seconds ago (UTC)", "2 minutes ago (UTC)", "3 hours ago (UTC)".
func TimeAgo(t time.Time) string {
	now := time.Now().UTC()
	t = t.UTC()

	diff := now.Sub(t)

	// Handle future times
	if diff < 0 {
		return "in the future (UTC)"
	}

	// Seconds
	if diff < time.Minute {
		seconds := int(diff.Seconds())
		if seconds == 1 {
			return "1 second ago (UTC)"
		}
		return fmt.Sprintf("%d seconds ago (UTC)", seconds)
	}

	// Minutes
	if diff < time.Hour {
		minutes := int(diff.Minutes())
		if minutes == 1 {
			return "1 minute ago (UTC)"
		}
		return fmt.Sprintf("%d minutes ago (UTC)", minutes)
	}

	// Hours
	if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago (UTC)"
		}
		return fmt.Sprintf("%d hours ago (UTC)", hours)
	}

	// Days
	days := int(diff.Hours() / 24)
	if days == 1 {
		return "1 day ago (UTC)"
	}
	return fmt.Sprintf("%d days ago (UTC)", days)
}

// FormatTimestamp returns a formatted timestamp string in UTC.
// Format: "2006-01-02 15:04:05 UTC".
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}
