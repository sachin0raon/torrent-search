package stream

import (
	"context"
	"errors"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
)

// TestOwnedByProtectedCategory covers Decision #24: the cross-process delete
// guard must protect *every* configured category (both the streaming
// engine's and the download manager's), not just one — a torrent tagged
// with either must be skipped.
func TestOwnedByProtectedCategory(t *testing.T) {
	tests := []struct {
		name       string
		protected  []string
		torrentCat string
		wantOwned  bool
	}{
		{"no protected categories configured", nil, "tsa-stream-engine", false},
		{"matches the only protected category", []string{"tsa-stream-engine"}, "tsa-stream-engine", true},
		{"matches the download category in a two-category set", []string{"tsa-stream-engine", "tsa-download"}, "tsa-download", true},
		{"matches the stream category in a two-category set", []string{"tsa-stream-engine", "tsa-download"}, "tsa-stream-engine", true},
		{"matches neither", []string{"tsa-stream-engine", "tsa-download"}, "unrelated-category", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const hash = "aaaa"
			fake := newFakeQbtAPI()
			fake.torrents[hash] = qbt.Torrent{Hash: hash, Category: tc.torrentCat}

			q := &QBitPeerSource{client: fake, protectedCategories: tc.protected}
			cat, owned := q.ownedByProtectedCategory(context.Background(), hash)
			if owned != tc.wantOwned {
				t.Errorf("owned = %v, want %v", owned, tc.wantOwned)
			}
			if owned && cat != tc.torrentCat {
				t.Errorf("matched category = %q, want %q", cat, tc.torrentCat)
			}
		})
	}

	t.Run("lookup failure fails safe (not owned)", func(t *testing.T) {
		fake := newFakeQbtAPI()
		fake.getTorrentsErr = errors.New("qbittorrent unreachable")
		q := &QBitPeerSource{client: fake, protectedCategories: []string{"tsa-stream-engine"}}
		_, owned := q.ownedByProtectedCategory(context.Background(), "aaaa")
		if owned {
			t.Error("a lookup failure must not report owned=true")
		}
	})

	t.Run("hash not found fails safe (not owned)", func(t *testing.T) {
		fake := newFakeQbtAPI()
		q := &QBitPeerSource{client: fake, protectedCategories: []string{"tsa-stream-engine"}}
		_, owned := q.ownedByProtectedCategory(context.Background(), "missing")
		if owned {
			t.Error("an unknown hash must not report owned=true")
		}
	})
}
