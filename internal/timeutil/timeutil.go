// Package timeutil provides utilities for time formatting and manipulation.
package timeutil

import (
	"context"
	"fmt"
	"time"
)

// FormatTimeAgo formats a timestamp as a relative time string
func FormatTimeAgo(timestamp time.Time) string {
	now := time.Now()
	diff := now.Sub(timestamp)

	switch {
	case diff < time.Minute:
		seconds := int(diff.Seconds())
		if seconds <= 0 {
			return "0 seconds ago"
		}
		if seconds == 1 {
			return "1 second ago"
		}
		return fmt.Sprintf("%d seconds ago", seconds)
	case diff < time.Hour:
		minutes := int(diff.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	default:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// Sleep blocks for d or until ctx is done, whichever comes first. Use it
// inside a polling loop in place of time.Sleep so cancellation is not delayed by
// up to a full poll interval.
func Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
