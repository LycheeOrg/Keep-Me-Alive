// Package tui implements the live-refreshing "-s" status view, a read-only
// Bubble Tea program that shows one line per host from the SQLite history
// written by the checker/daemon.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/LycheeOrg/Keep-Me-Alive/internal/config"
	"github.com/LycheeOrg/Keep-Me-Alive/internal/store"
)

const (
	sparklineLen = 20
	refreshEvery = 2 * time.Second
)

var (
	upStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	downStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))

	nameStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Width(20)
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))

	localTypeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Width(7)
	remoteTypeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Width(7)

	upBadgeStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("42")).Width(8).Align(lipgloss.Center)
	downBadgeStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("196")).Width(8).Align(lipgloss.Center)
	neverBadgeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("244")).Width(8).Align(lipgloss.Center)
)

// typeStyle picks the accent color for a site's local/remote badge.
func typeStyle(t config.SiteType) lipgloss.Style {
	if t == config.SiteLocal {
		return localTypeStyle
	}
	return remoteTypeStyle
}

// latencyStyle color-tiers a latency value: fast (green), sluggish (yellow),
// slow (red).
func latencyStyle(d time.Duration) lipgloss.Style {
	switch {
	case d >= time.Second:
		return downStyle
	case d >= 200*time.Millisecond:
		return warnStyle
	default:
		return upStyle
	}
}

// row is one rendered line of the status table.
type row struct {
	Name      string
	Type      config.SiteType
	Up        bool
	HasData   bool
	CheckedAt time.Time
	Latency   time.Duration
	Err       string
	History   []bool // oldest first
}

type rowsMsg struct {
	rows []row
	err  error
}

type tickMsg time.Time

type model struct {
	ctx   context.Context
	store *store.Store
	sites []config.SiteConfig
	rows  []row
	err   error
}

func newModel(ctx context.Context, st *store.Store, sites []config.SiteConfig) model {
	return model{ctx: ctx, store: st, sites: sites}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.fetchCmd(), tickCmd())
}

func (m model) fetchCmd() tea.Cmd {
	return func() tea.Msg {
		rows, err := fetchRows(m.ctx, m.store, m.sites)
		return rowsMsg{rows: rows, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	case tickMsg:
		return m, tea.Batch(m.fetchCmd(), tickCmd())
	case rowsMsg:
		m.rows = msg.rows
		m.err = msg.err
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("keep-me-alive status") + "\n\n")

	if m.err != nil {
		b.WriteString(downStyle.Render(fmt.Sprintf("error reading history: %v", m.err)) + "\n")
	}
	if len(m.rows) == 0 {
		b.WriteString(dimStyle.Render("no check history yet") + "\n")
	}
	for _, r := range m.rows {
		b.WriteString(renderRow(r) + "\n")
	}

	b.WriteString("\n" + titleStyle.Render("q") + dimStyle.Render(" to quit · refreshes every 2s"))
	return b.String()
}

func renderRow(r row) string {
	badge := neverBadgeStyle.Render("NEVER")
	checkedAgo := dimStyle.Width(14).Render("—")
	latency := dimStyle.Width(10).Render("—")

	if r.HasData {
		if r.Up {
			badge = upBadgeStyle.Render("UP")
		} else {
			badge = downBadgeStyle.Render("DOWN")
		}
		checkedAgo = dimStyle.Width(14).Render(time.Since(r.CheckedAt).Round(time.Second).String() + " ago")
		latency = latencyStyle(r.Latency).Width(10).Render(r.Latency.Round(time.Millisecond).String())
	}

	return fmt.Sprintf("%s %s %s  %s  %s  %s",
		nameStyle.Render(r.Name), typeStyle(r.Type).Render(string(r.Type)), badge, checkedAgo, latency, renderSparkline(r.History))
}

func renderSparkline(history []bool) string {
	var b strings.Builder
	for _, up := range history {
		if up {
			b.WriteString(upStyle.Render("●"))
		} else {
			b.WriteString(downStyle.Render("✗"))
		}
	}
	return b.String()
}

func fetchRows(ctx context.Context, st *store.Store, sites []config.SiteConfig) ([]row, error) {
	latest, err := st.LatestPerSite(ctx)
	if err != nil {
		return nil, err
	}
	latestByName := make(map[string]store.CheckRecord, len(latest))
	for _, rec := range latest {
		latestByName[rec.SiteName] = rec
	}

	rows := make([]row, 0, len(sites))
	for _, site := range sites {
		r := row{Name: site.Name, Type: site.Type}
		if rec, ok := latestByName[site.Name]; ok {
			r.HasData = true
			r.Up = rec.Up
			r.CheckedAt = rec.CheckedAt
			r.Latency = rec.Latency
			r.Err = rec.Err
		}

		recent, err := st.Recent(ctx, site.Name, sparklineLen)
		if err == nil {
			hist := make([]bool, len(recent))
			for i, rec := range recent {
				hist[len(recent)-1-i] = rec.Up
			}
			r.History = hist
		}

		rows = append(rows, r)
	}
	return rows, nil
}

// Run launches the status TUI, blocking until the user quits or ctx is
// cancelled. It performs no checks itself — it only reads history written
// by other invocations (-o, -d, or verbose mode).
func Run(ctx context.Context, st *store.Store, sites []config.SiteConfig) error {
	p := tea.NewProgram(newModel(ctx, st, sites), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}
