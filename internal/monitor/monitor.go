// Package monitor orchestrates health checks, local-site restarts, Signal
// notifications, and history persistence according to the per-site state
// machine (see checkSite).
package monitor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"text/tabwriter"
	"time"

	"github.com/LycheeOrg/Keep-Me-Alive/internal/checker"
	"github.com/LycheeOrg/Keep-Me-Alive/internal/config"
	"github.com/LycheeOrg/Keep-Me-Alive/internal/restart"
	"github.com/LycheeOrg/Keep-Me-Alive/internal/store"
)

// restartTimeout bounds how long a local restart command may run.
const restartTimeout = 30 * time.Second

// CheckerFunc matches checker.Check's signature, injectable for testing.
type CheckerFunc func(ctx context.Context, url string, timeout time.Duration) checker.Result

// RestarterFunc matches restart.Run's signature, injectable for testing.
type RestarterFunc func(ctx context.Context, command, workingDir string, timeout time.Duration) (string, error)

// Notifier sends a Signal notification message.
type Notifier interface {
	Send(ctx context.Context, message string) error
}

// SiteResult is the outcome of one full check cycle for a site (including
// any restart-and-recheck grace flow), used for both persistence and the
// one-time-mode results table.
type SiteResult struct {
	Site      config.SiteConfig
	Up        bool
	CheckedAt time.Time
	Latency   time.Duration
	Err       string
	Restarted bool
	Notified  bool
}

// Monitor ties together checking, restarting, notifying, and persisting.
type Monitor struct {
	cfg      *config.Config
	check    CheckerFunc
	restart  RestarterFunc
	notifier Notifier
	store    *store.Store
	logger   *slog.Logger
	states   map[string]*SiteState
}

// New builds a Monitor using the real checker.Check and restart.Run
// implementations.
func New(cfg *config.Config, notifier Notifier, st *store.Store, logger *slog.Logger) *Monitor {
	return &Monitor{
		cfg:      cfg,
		check:    checker.Check,
		restart:  restart.Run,
		notifier: notifier,
		store:    st,
		logger:   logger,
		states:   make(map[string]*SiteState),
	}
}

// RunOnce performs exactly one check pass over all sites using a fresh,
// empty state map — each invocation is independent, with no state carried
// over from prior runs.
func (m *Monitor) RunOnce(ctx context.Context) []SiteResult {
	states := make(map[string]*SiteState, len(m.cfg.Sites))
	return m.runCycle(ctx, states)
}

// RunDaemon loops on cfg.CheckInterval, reusing m.states across cycles so
// notifications only fire on state transitions, until ctx is cancelled.
func (m *Monitor) RunDaemon(ctx context.Context) {
	m.runCycle(ctx, m.states)

	ticker := time.NewTicker(m.cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runCycle(ctx, m.states)
		}
	}
}

func (m *Monitor) runCycle(ctx context.Context, states map[string]*SiteState) []SiteResult {
	results := make([]SiteResult, 0, len(m.cfg.Sites))
	for _, site := range m.cfg.Sites {
		select {
		case <-ctx.Done():
			return results
		default:
		}
		results = append(results, m.checkSite(ctx, site, states))
	}
	return results
}

// checkSite implements the per-site state machine:
//
// Remote: up → notify recovery only if previously Down; down → notify only
// on the first cycle detecting the outage (prevStatus != Down).
//
// Local: same up-branch as remote. On down, the restart-and-recheck grace
// flow runs every cycle (regardless of prevStatus) so restarts keep being
// retried while broken; the notification is still deduped on prevStatus so
// it fires only once per outage.
func (m *Monitor) checkSite(ctx context.Context, site config.SiteConfig, states map[string]*SiteState) SiteResult {
	state, ok := states[site.Name]
	if !ok {
		state = &SiteState{Name: site.Name}
		states[site.Name] = state
	}
	prevStatus := state.Status

	res := m.check(ctx, site.URL, m.cfg.HTTPTimeout)
	result := toResult(site, res)

	if res.Up {
		m.logger.Debug("check ok", "site", site.Name, "latency", res.Latency)
		if prevStatus == StatusDown {
			m.notify(ctx, recoveryMessage(site))
			result.Notified = true
			m.logger.Info("site recovered", "site", site.Name)
		}
		m.finish(ctx, site, state, result)
		return result
	}

	m.logger.Debug("check failed", "site", site.Name, "err", result.Err)

	if site.Type == config.SiteRemote {
		if prevStatus == StatusDown {
			m.logger.Info("site still down", "site", site.Name)
		} else {
			m.notify(ctx, downMessage(site, result.Err, false, nil))
			result.Notified = true
			m.logger.Info("site down, notified", "site", site.Name)
		}
		m.finish(ctx, site, state, result)
		return result
	}

	// Local site: restart-and-recheck grace flow.
	m.logger.Info("site down, restarting", "site", site.Name)
	output, restartErr := m.restart(ctx, site.RestartCommand, site.WorkingDir, restartTimeout)
	result.Restarted = true
	if restartErr != nil {
		m.logger.Error("restart command failed", "site", site.Name, "err", restartErr, "output", output)
	} else {
		m.logger.Debug("restart command succeeded", "site", site.Name, "output", output)
	}

	select {
	case <-ctx.Done():
		m.finish(ctx, site, state, result)
		return result
	case <-time.After(m.cfg.RestartRecheckDelay):
	}

	res2 := m.check(ctx, site.URL, m.cfg.HTTPTimeout)
	result = toResult(site, res2)
	result.Restarted = true

	if res2.Up {
		if prevStatus == StatusDown {
			m.notify(ctx, recoveryMessage(site))
			result.Notified = true
			m.logger.Info("site recovered after restart", "site", site.Name)
		} else {
			m.logger.Info("site recovered after restart, no notification needed", "site", site.Name)
		}
	} else {
		if prevStatus != StatusDown {
			m.notify(ctx, downMessage(site, result.Err, true, restartErr))
			result.Notified = true
			m.logger.Info("site still down after restart, notified", "site", site.Name)
		} else {
			m.logger.Info("site still down, restart retried", "site", site.Name)
		}
	}

	m.finish(ctx, site, state, result)
	return result
}

func toResult(site config.SiteConfig, res checker.Result) SiteResult {
	r := SiteResult{
		Site:      site,
		Up:        res.Up,
		CheckedAt: res.CheckedAt,
		Latency:   res.Latency,
	}
	if res.Err != nil {
		r.Err = res.Err.Error()
	}
	return r
}

// finish updates in-memory state to match result and persists it to the store.
func (m *Monitor) finish(ctx context.Context, site config.SiteConfig, state *SiteState, result SiteResult) {
	if result.Up {
		state.Status = StatusUp
	} else {
		state.Status = StatusDown
	}
	state.LastCheckedAt = result.CheckedAt
	state.LastError = result.Err

	if m.store == nil {
		return
	}
	rec := store.CheckRecord{
		SiteName:  site.Name,
		SiteType:  site.Type,
		CheckedAt: result.CheckedAt,
		Up:        result.Up,
		Latency:   result.Latency,
		Err:       result.Err,
		Restarted: result.Restarted,
		Notified:  result.Notified,
	}
	// Persistence is a fast, local, best-effort write: use a context that's
	// detached from ctx's cancellation (e.g. a shutdown signal mid-cycle)
	// so the last known result still gets flushed instead of failing with
	// "context canceled" right as the process exits.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := m.store.Record(persistCtx, rec, m.cfg.HistorySize); err != nil {
		m.logger.Error("failed to persist check result", "site", site.Name, "err", err)
	}
}

func (m *Monitor) notify(ctx context.Context, message string) {
	if m.notifier == nil {
		return
	}
	if err := m.notifier.Send(ctx, message); err != nil {
		m.logger.Error("failed to send signal notification", "err", err)
	}
}

// downMessage builds the Signal notification text for a site that's down.
// restartErr, when non-nil, is the error from a failed restart command
// attempt and is appended so the operator knows the automated fix didn't
// even run successfully, not just that the site is still unreachable.
func downMessage(site config.SiteConfig, errText string, afterRestart bool, restartErr error) string {
	if !afterRestart {
		return fmt.Sprintf("🔴 %s is DOWN\nURL: %s\nError: %s", site.Name, site.URL, errText)
	}
	msg := fmt.Sprintf("🔴 %s is DOWN (restart attempted, still unreachable)\nURL: %s\nError: %s", site.Name, site.URL, errText)
	if restartErr != nil {
		msg += fmt.Sprintf("\nRestart command failed: %s", restartErr)
	}
	return msg
}

func recoveryMessage(site config.SiteConfig) string {
	return fmt.Sprintf("🟢 %s has RECOVERED\nURL: %s", site.Name, site.URL)
}

// PrintResults writes a human-readable results table to w, used by -o mode.
func PrintResults(w io.Writer, results []SiteResult) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTYPE\tSTATUS\tLATENCY\tRESTARTED\tNOTIFIED\tERROR")
	for _, r := range results {
		status := "UP"
		if !r.Up {
			status = "DOWN"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%v\t%v\t%s\n",
			r.Site.Name, r.Site.Type, status, r.Latency, r.Restarted, r.Notified, r.Err)
	}
	tw.Flush()
}
