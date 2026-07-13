// Package store persists a bounded history of site check results to SQLite
// so the status TUI can display recent activity independently of whether a
// checking process is currently running.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/LycheeOrg/Keep-Me-Alive/internal/config"
)

const schema = `
CREATE TABLE IF NOT EXISTS checks (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	site_name  TEXT NOT NULL,
	site_type  TEXT NOT NULL,
	checked_at DATETIME NOT NULL,
	up         BOOLEAN NOT NULL,
	latency_ms INTEGER NOT NULL,
	error      TEXT NOT NULL DEFAULT '',
	restarted  BOOLEAN NOT NULL DEFAULT 0,
	notified   BOOLEAN NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_checks_site_time ON checks (site_name, checked_at);
`

// CheckRecord is one persisted check result for a single site.
type CheckRecord struct {
	SiteName  string
	SiteType  config.SiteType
	CheckedAt time.Time
	Up        bool
	Latency   time.Duration
	Err       string
	Restarted bool
	Notified  bool
}

// Store wraps a SQLite database holding the check history ring buffer.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path, enables
// WAL mode so readers (the status TUI) don't block writers (the checker),
// and ensures the schema exists.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: opening %s: %w", path, err)
	}

	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: enabling WAL mode: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: creating schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Record inserts rec, then prunes older rows for that site so at most
// historySize records remain per site name (a fixed-size ring buffer).
func (s *Store) Record(ctx context.Context, rec CheckRecord, historySize int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO checks (site_name, site_type, checked_at, up, latency_ms, error, restarted, notified)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, rec.SiteName, string(rec.SiteType), rec.CheckedAt, rec.Up, rec.Latency.Milliseconds(), rec.Err, rec.Restarted, rec.Notified)
	if err != nil {
		return fmt.Errorf("store: insert: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		DELETE FROM checks
		WHERE site_name = ?
		AND id NOT IN (
			SELECT id FROM checks WHERE site_name = ? ORDER BY checked_at DESC, id DESC LIMIT ?
		)
	`, rec.SiteName, rec.SiteName, historySize)
	if err != nil {
		return fmt.Errorf("store: prune: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}

	return nil
}

// Recent returns up to limit most recent records for siteName, newest first.
func (s *Store) Recent(ctx context.Context, siteName string, limit int) ([]CheckRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT site_name, site_type, checked_at, up, latency_ms, error, restarted, notified
		FROM checks
		WHERE site_name = ?
		ORDER BY checked_at DESC, id DESC
		LIMIT ?
	`, siteName, limit)
	if err != nil {
		return nil, fmt.Errorf("store: query recent: %w", err)
	}
	defer rows.Close()

	return scanRecords(rows)
}

// LatestPerSite returns the single most recent record for every distinct
// site present in the database, ordered by site name.
func (s *Store) LatestPerSite(ctx context.Context) ([]CheckRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.site_name, c.site_type, c.checked_at, c.up, c.latency_ms, c.error, c.restarted, c.notified
		FROM checks c
		INNER JOIN (
			SELECT site_name, MAX(checked_at) AS max_checked_at
			FROM checks
			GROUP BY site_name
		) latest ON c.site_name = latest.site_name AND c.checked_at = latest.max_checked_at
		ORDER BY c.site_name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: query latest per site: %w", err)
	}
	defer rows.Close()

	return scanRecords(rows)
}

func scanRecords(rows *sql.Rows) ([]CheckRecord, error) {
	var records []CheckRecord
	for rows.Next() {
		var (
			rec       CheckRecord
			siteType  string
			checkedAt time.Time
			latencyMs int64
		)
		if err := rows.Scan(&rec.SiteName, &siteType, &checkedAt, &rec.Up, &latencyMs, &rec.Err, &rec.Restarted, &rec.Notified); err != nil {
			return nil, fmt.Errorf("store: scan row: %w", err)
		}
		rec.SiteType = config.SiteType(siteType)
		rec.CheckedAt = checkedAt
		rec.Latency = time.Duration(latencyMs) * time.Millisecond
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating rows: %w", err)
	}
	return records, nil
}
