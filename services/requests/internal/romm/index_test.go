package romm

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResemblingFindsTheSameGameUnderAnotherEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bundle := 37151
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"id": 1, "igdb_id": bundle, "name": "Mario & Luigi: Superstar Saga + Bowser's Minions", "platform_fs_slug": "3ds"},
			{"id": 2, "name": "", "fs_name_no_ext": "Mario Kart 7 (Europe)", "platform_fs_slug": "3ds"},
			{"id": 3, "name": "Gone Game", "platform_fs_slug": "3ds", "missing_from_fs": true},
		}})
	}))
	defer srv.Close()
	x := NewIndex(New(srv.URL, "t", time.Minute), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := x.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n, ok := x.Resembling("3ds", "Mario & Luigi: Superstar Saga"); !ok || n.RomID != 1 {
		t.Errorf("remake entry should resemble the bundle: %v %v", n, ok)
	}
	if n, ok := x.Resembling("3ds", "Mario Kart 7"); !ok || n.RomID != 2 {
		t.Errorf("unmatched rom by file name: %v %v", n, ok)
	}
	if n, ok := x.Resembling("3ds", "Mario Kart"); !ok || n.RomID != 2 {
		t.Errorf("a title the rom name starts with should warn: %v %v", n, ok)
	}
	for _, miss := range []struct{ plat, title string }{
		{"gba", "Mario & Luigi: Superstar Saga"}, // other platform
		{"3ds", "Mario"},                         // one word: exact only
		{"3ds", "Mario & Luigi: Dream Team"},     // another game of the series
		{"3ds", "Gone Game"},                     // missing from disk
	} {
		if n, ok := x.Resembling(miss.plat, miss.title); ok {
			t.Errorf("%s on %s should not resemble anything, got %v", miss.title, miss.plat, n)
		}
	}
}
