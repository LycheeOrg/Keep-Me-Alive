// Command keep-me-alive periodically checks a configured list of websites,
// restarts local ones that go down, and notifies a Signal group via
// signal-cli-rest-api on state changes.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	ossignal "os/signal"
	"syscall"
	"time"

	"github.com/LycheeOrg/Keep-Me-Alive/internal/config"
	"github.com/LycheeOrg/Keep-Me-Alive/internal/monitor"
	"github.com/LycheeOrg/Keep-Me-Alive/internal/signal"
	"github.com/LycheeOrg/Keep-Me-Alive/internal/store"
	"github.com/LycheeOrg/Keep-Me-Alive/internal/tui"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		oneTime    bool
		daemon     bool
		status     bool
		testSignal bool
		configPath string
	)
	flag.BoolVar(&oneTime, "o", false, "run one-time check and exit")
	flag.BoolVar(&daemon, "d", false, "run as daemon at the configured interval")
	flag.BoolVar(&status, "s", false, "show the live status TUI")
	flag.BoolVar(&testSignal, "t", false, "send a test message to the configured Signal endpoint and exit")
	flag.StringVar(&configPath, "c", "config.yaml", "path to config file")
	flag.StringVar(&configPath, "config", "config.yaml", "path to config file")
	flag.Parse()

	modeCount := 0
	for _, v := range []bool{oneTime, daemon, status, testSignal} {
		if v {
			modeCount++
		}
	}
	if modeCount > 1 {
		fmt.Fprintln(os.Stderr, "error: -o, -d, -s, and -t are mutually exclusive")
		flag.Usage()
		return 2
	}

	level := slog.LevelInfo
	if !oneTime && !daemon && !status && !testSignal {
		level = slog.LevelDebug // no-flags mode = verbose daemon
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Ignore SIGHUP so a daemon started directly over SSH (without nohup/tmux/
	// systemd) keeps running after the session that launched it disconnects.
	ossignal.Ignore(syscall.SIGHUP)

	ctx, stop := ossignal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sigClient := signal.New(cfg.Signal, cfg.HTTPTimeout)

	if testSignal {
		message := fmt.Sprintf("🔔 keep-me-alive test message\nSent at: %s\nIf you received this, your Signal notification config is working.", time.Now().Format(time.RFC3339))
		if err := sigClient.Send(ctx, message); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Println("test message sent successfully")
		return 0
	}

	st, err := store.Open(cfg.StateDBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer st.Close()

	if status {
		if err := tui.Run(ctx, st, cfg.Sites); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}

	mon := monitor.New(cfg, sigClient, st, logger)

	if oneTime {
		results := mon.RunOnce(ctx)
		monitor.PrintResults(os.Stdout, results)
		return 0
	}

	// -d or no-flags: daemon mode (quiet or verbose depending on level above).
	mon.RunDaemon(ctx)
	return 0
}
