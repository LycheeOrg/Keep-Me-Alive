package monitor

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LycheeOrg/Keep-Me-Alive/internal/config"
	"github.com/LycheeOrg/Keep-Me-Alive/internal/store"
)

// fakeNotifier records every message sent to it.
type fakeNotifier struct {
	mu       sync.Mutex
	messages []string
}

func (f *fakeNotifier) Send(ctx context.Context, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, msg)
	return nil
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}

func (f *fakeNotifier) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.messages) == 0 {
		return ""
	}
	return f.messages[len(f.messages)-1]
}

// newToggleServer returns a server whose up/down response is flipped with SetUp.
func newToggleServer(t *testing.T, up bool) (*httptest.Server, func(bool)) {
	t.Helper()
	var isUp atomic.Bool
	isUp.Store(up)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isUp.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func(v bool) { isUp.Store(v) }
}

// newMarkerServer returns a server that reports up if and only if markerPath exists.
func newMarkerServer(t *testing.T, markerPath string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(markerPath); err == nil {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestMonitor(t *testing.T, sites []config.SiteConfig) (*Monitor, *fakeNotifier) {
	t.Helper()

	cfg := &config.Config{
		CheckInterval:       10 * time.Millisecond,
		RestartRecheckDelay: 10 * time.Millisecond,
		HTTPTimeout:         time.Second,
		HistorySize:         50,
		Sites:               sites,
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open() unexpected error: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	notifier := &fakeNotifier{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, notifier, st, logger), notifier
}

func remoteSite(name, url string) config.SiteConfig {
	return config.SiteConfig{Name: name, Type: config.SiteRemote, URL: url}
}

func localSite(name, url, restartCommand, workingDir string) config.SiteConfig {
	return config.SiteConfig{Name: name, Type: config.SiteLocal, URL: url, RestartCommand: restartCommand, WorkingDir: workingDir}
}

func TestMonitor_RemoteDown_NotifiesOnce(t *testing.T) {
	srv, _ := newToggleServer(t, false)
	site := remoteSite("remote-a", srv.URL)
	mon, notifier := newTestMonitor(t, []config.SiteConfig{site})

	mon.checkSite(context.Background(), site, mon.states)

	if got := notifier.count(); got != 1 {
		t.Fatalf("notifications = %d, want 1", got)
	}
}

func TestMonitor_RemoteDownTwice_StillOneNotification(t *testing.T) {
	srv, _ := newToggleServer(t, false)
	site := remoteSite("remote-a", srv.URL)
	mon, notifier := newTestMonitor(t, []config.SiteConfig{site})

	mon.checkSite(context.Background(), site, mon.states)
	mon.checkSite(context.Background(), site, mon.states)

	if got := notifier.count(); got != 1 {
		t.Fatalf("notifications = %d, want 1 (no repeat while still down)", got)
	}
}

func TestMonitor_RemoteDownThenUp_TwoNotifications(t *testing.T) {
	srv, setUp := newToggleServer(t, false)
	site := remoteSite("remote-a", srv.URL)
	mon, notifier := newTestMonitor(t, []config.SiteConfig{site})

	mon.checkSite(context.Background(), site, mon.states)
	setUp(true)
	mon.checkSite(context.Background(), site, mon.states)

	if got := notifier.count(); got != 2 {
		t.Fatalf("notifications = %d, want 2 (down + recovery)", got)
	}
	if last := notifier.last(); !strings.Contains(last, "RECOVERED") {
		t.Errorf("last message = %q, want to mention RECOVERED", last)
	}
}

func TestMonitor_LocalDown_RecoversWithinGrace_NoNotification(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "up-marker")
	srv := newMarkerServer(t, marker)
	site := localSite("local-a", srv.URL, "touch "+marker, dir)
	mon, notifier := newTestMonitor(t, []config.SiteConfig{site})

	result := mon.checkSite(context.Background(), site, mon.states)

	if !result.Restarted {
		t.Error("result.Restarted = false, want true")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("expected restart command to create marker file: %v", err)
	}
	if !result.Up {
		t.Error("result.Up = false, want true (self-healed within grace delay)")
	}
	if got := notifier.count(); got != 0 {
		t.Fatalf("notifications = %d, want 0 (self-heal on first detection is silent)", got)
	}
}

func TestMonitor_LocalDown_StaysDown_NotifiesOnce(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "never-created")
	restartMarker := filepath.Join(dir, "restart-ran")
	srv := newMarkerServer(t, marker)
	site := localSite("local-a", srv.URL, "touch "+restartMarker, dir)
	mon, notifier := newTestMonitor(t, []config.SiteConfig{site})

	result := mon.checkSite(context.Background(), site, mon.states)

	if _, err := os.Stat(restartMarker); err != nil {
		t.Errorf("expected restart command to run: %v", err)
	}
	if result.Up {
		t.Error("result.Up = true, want false (still down after restart)")
	}
	if got := notifier.count(); got != 1 {
		t.Fatalf("notifications = %d, want 1", got)
	}
}

func TestMonitor_LocalStillDownAcrossCycles_RestartRetriedOneNotification(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "never-created")
	counter := filepath.Join(dir, "restart-count")
	srv := newMarkerServer(t, marker)
	site := localSite("local-a", srv.URL, "printf x >> "+counter, dir)
	mon, notifier := newTestMonitor(t, []config.SiteConfig{site})

	mon.checkSite(context.Background(), site, mon.states)
	mon.checkSite(context.Background(), site, mon.states)

	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("reading restart counter file: %v", err)
	}
	if len(data) != 2 {
		t.Errorf("restart invocation count = %d, want 2 (retried every cycle)", len(data))
	}
	if got := notifier.count(); got != 1 {
		t.Fatalf("notifications = %d, want 1 (deduped across cycles)", got)
	}
}

func TestMonitor_LocalDownNotifiedThenRecovers_OneRecoveryNotification(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "recovery-marker")
	srv := newMarkerServer(t, marker)
	// Cycle 1's restart doesn't create the marker, so it stays down and notifies.
	site := localSite("local-a", srv.URL, "true", dir)
	mon, notifier := newTestMonitor(t, []config.SiteConfig{site})

	mon.checkSite(context.Background(), site, mon.states)
	if got := notifier.count(); got != 1 {
		t.Fatalf("after cycle 1: notifications = %d, want 1", got)
	}

	// Simulate an external fix between cycles.
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("writing recovery marker: %v", err)
	}

	mon.checkSite(context.Background(), site, mon.states)
	if got := notifier.count(); got != 2 {
		t.Fatalf("after cycle 2: notifications = %d, want 2", got)
	}
	if last := notifier.last(); !strings.Contains(last, "RECOVERED") {
		t.Errorf("last message = %q, want to mention RECOVERED", last)
	}
}

func TestMonitor_RunOnce_NoCrossCallState(t *testing.T) {
	srv, _ := newToggleServer(t, false)
	site := remoteSite("remote-a", srv.URL)
	mon, notifier := newTestMonitor(t, []config.SiteConfig{site})

	mon.RunOnce(context.Background())
	mon.RunOnce(context.Background())

	if got := notifier.count(); got != 2 {
		t.Fatalf("notifications = %d, want 2 (each RunOnce call is independent)", got)
	}
}
