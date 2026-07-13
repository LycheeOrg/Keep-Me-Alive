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
		configPath string
	)
	flag.BoolVar(&oneTime, "o", false, "run one-time check and exit")
	flag.BoolVar(&daemon, "d", false, "run as daemon at the configured interval")
	flag.BoolVar(&status, "s", false, "show the live status TUI")
	flag.StringVar(&configPath, "c", "config.yaml", "path to config file")
	flag.StringVar(&configPath, "config", "config.yaml", "path to config file")
	flag.Parse()

	modeCount := 0
	for _, v := range []bool{oneTime, daemon, status} {
		if v {
			modeCount++
		}
	}
	if modeCount > 1 {
		fmt.Fprintln(os.Stderr, "error: -o, -d, and -s are mutually exclusive")
		flag.Usage()
		return 2
	}

	level := slog.LevelInfo
	if !oneTime && !daemon && !status {
		level = slog.LevelDebug // no-flags mode = verbose daemon
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	ctx, stop := ossignal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	sigClient := signal.New(cfg.Signal, cfg.HTTPTimeout)
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
