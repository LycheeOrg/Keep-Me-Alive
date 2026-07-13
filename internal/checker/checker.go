// Package checker performs HTTP health checks against monitored site URLs.
package checker

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Result is the outcome of a single health check.
type Result struct {
	Up        bool
	CheckedAt time.Time
	Latency   time.Duration
	Err       error
}

// Check performs a single GET against url and reports Up if the response
// status is 2xx and arrives within timeout. Any non-2xx status, network
// error, or timeout is reported as down with Err explaining why.
func Check(ctx context.Context, url string, timeout time.Duration) Result {
	checkedAt := time.Now()

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return Result{Up: false, CheckedAt: checkedAt, Err: fmt.Errorf("building request: %w", err)}
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return Result{Up: false, CheckedAt: checkedAt, Latency: latency, Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{
			Up:        false,
			CheckedAt: checkedAt,
			Latency:   latency,
			Err:       fmt.Errorf("unexpected status: %s", resp.Status),
		}
	}

	return Result{Up: true, CheckedAt: checkedAt, Latency: latency}
}
