package tui

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/LycheeOrg/Keep-Me-Alive/internal/config"
	"github.com/LycheeOrg/Keep-Me-Alive/internal/store"
)

func TestFetchRows(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open() unexpected error: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	base := time.Now()

	// site-a: down, then up (most recent).
	for i, up := range []bool{false, true} {
		rec := store.CheckRecord{
			SiteName:  "site-a",
			SiteType:  config.SiteRemote,
			CheckedAt: base.Add(time.Duration(i) * time.Second),
			Up:        up,
		}
		if err := st.Record(ctx, rec, 50); err != nil {
			t.Fatalf("Record() unexpected error: %v", err)
		}
	}

	sites := []config.SiteConfig{
		{Name: "site-a", Type: config.SiteRemote, URL: "http://example.com"},
		{Name: "site-b", Type: config.SiteLocal, URL: "http://example.org"}, // never checked
	}

	rows, err := fetchRows(ctx, st, sites)
	if err != nil {
		t.Fatalf("fetchRows() unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}

	a := rows[0]
	if !a.HasData {
		t.Fatal("site-a HasData = false, want true")
	}
	if !a.Up {
		t.Error("site-a Up = false, want true (latest record was up)")
	}
	if len(a.History) != 2 {
		t.Fatalf("site-a History len = %d, want 2", len(a.History))
	}
	if a.History[0] != false || a.History[1] != true {
		t.Errorf("site-a History = %v, want [false true] (oldest first)", a.History)
	}

	b := rows[1]
	if b.HasData {
		t.Error("site-b HasData = true, want false (never checked)")
	}
	if len(b.History) != 0 {
		t.Errorf("site-b History = %v, want empty", b.History)
	}
}
