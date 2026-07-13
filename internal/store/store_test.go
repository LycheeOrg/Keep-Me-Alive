package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/LycheeOrg/Keep-Me-Alive/internal/config"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_RecordAndPrune(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	const historySize = 5
	base := time.Now()
	for i := 0; i < historySize+3; i++ {
		rec := CheckRecord{
			SiteName:  "site-a",
			SiteType:  config.SiteRemote,
			CheckedAt: base.Add(time.Duration(i) * time.Second),
			Up:        i%2 == 0,
		}
		if err := s.Record(ctx, rec, historySize); err != nil {
			t.Fatalf("Record() unexpected error: %v", err)
		}
	}

	recent, err := s.Recent(ctx, "site-a", 100)
	if err != nil {
		t.Fatalf("Recent() unexpected error: %v", err)
	}
	if len(recent) != historySize {
		t.Fatalf("len(recent) = %d, want %d (ring buffer should prune oldest)", len(recent), historySize)
	}

	// Newest first.
	for i := 0; i < len(recent)-1; i++ {
		if recent[i].CheckedAt.Before(recent[i+1].CheckedAt) {
			t.Fatalf("Recent() not ordered newest-first: %v before %v", recent[i].CheckedAt, recent[i+1].CheckedAt)
		}
	}

	// The oldest 3 records (i=0,1,2) should have been pruned; newest should remain.
	wantNewest := base.Add(time.Duration(historySize+2) * time.Second)
	if !recent[0].CheckedAt.Equal(wantNewest) {
		t.Errorf("newest record CheckedAt = %v, want %v", recent[0].CheckedAt, wantNewest)
	}
}

func TestStore_LatestPerSite(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	base := time.Now()
	sites := []string{"site-a", "site-b"}
	for _, name := range sites {
		for i := 0; i < 3; i++ {
			rec := CheckRecord{
				SiteName:  name,
				SiteType:  config.SiteLocal,
				CheckedAt: base.Add(time.Duration(i) * time.Second),
				Up:        i == 2,
			}
			if err := s.Record(ctx, rec, 50); err != nil {
				t.Fatalf("Record() unexpected error: %v", err)
			}
		}
	}

	latest, err := s.LatestPerSite(ctx)
	if err != nil {
		t.Fatalf("LatestPerSite() unexpected error: %v", err)
	}
	if len(latest) != 2 {
		t.Fatalf("len(latest) = %d, want 2", len(latest))
	}
	for _, rec := range latest {
		if !rec.Up {
			t.Errorf("site %s: latest record Up = false, want true (last recorded was up)", rec.SiteName)
		}
	}
}
