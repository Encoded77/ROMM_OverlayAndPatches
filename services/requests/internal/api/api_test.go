package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/engine"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/events"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/igdb"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/importer"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/notify"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/romm"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/settings"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/store"
)

// fakeRomm answers /api/users/me per cookie and /api/platforms.
func fakeRomm(t *testing.T) *httptest.Server {
	users := map[string]romm.User{
		"session=alice": {ID: 1, Username: "alice", Role: "viewer"},
		"session=bob":   {ID: 2, Username: "bob", Role: "viewer"},
		"session=root":  {ID: 3, Username: "root", Role: "admin"},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/users/me":
			u, ok := users[r.Header.Get("Cookie")]
			if !ok {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			json.NewEncoder(w).Encode(u)
		case "/api/platforms":
			gba := 24
			json.NewEncoder(w).Encode([]romm.Platform{{ID: 2, Slug: "gba", FSSlug: "gba", Name: "Game Boy Advance", IGDBID: &gba}})
		case "/api/roms":
			// The library holds IGDB game 1 on GBA.
			one := 1
			json.NewEncoder(w).Encode(map[string]any{"items": []romm.Rom{{ID: 77, FSName: "Golden Sun.gba", IGDBID: &one}}})
		default:
			http.NotFound(w, r)
		}
	}))
}

func setup(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	rommSrv := fakeRomm(t)
	t.Cleanup(rommSrv.Close)
	db, err := store.Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	held, err := settings.Load(context.Background(), db, settings.Defaults("", ""))
	if err != nil {
		t.Fatal(err)
	}
	rc := romm.New(rommSrv.URL, "token", time.Minute)
	hub := events.NewHub()
	ntfy := notify.New("", "", "", log)
	tmp := t.TempDir()
	eng := &engine.Engine{Store: db, Hub: hub, Romm: rc, Settings: held, Notify: ntfy, Log: log,
		Importer: importer.New(filepath.Join(tmp, "staging"), filepath.Join(tmp, "lib"), "")}
	igdbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			json.NewEncoder(w).Encode(map[string]any{"access_token": "x", "expires_in": 3600})
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "name": "Golden Sun", "platforms": []int{24}}})
	}))
	t.Cleanup(igdbSrv.Close)
	ig := igdb.New("id", "secret")
	ig.TokenURL, ig.APIURL = igdbSrv.URL+"/token", igdbSrv.URL
	s := &Server{Romm: rc, IGDB: ig, Store: db, Hub: hub, Log: log, Engine: eng, Index: romm.NewIndex(rc, log),
		Settings: held, Notify: ntfy}
	srv := httptest.NewServer(s.Handler("/ext/requests"))
	t.Cleanup(srv.Close)
	return srv, db
}

func call(t *testing.T, srv *httptest.Server, method, path, cookie, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+"/ext/requests"+path, strings.NewReader(body))
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestRequiresRommSession(t *testing.T) {
	srv, _ := setup(t)
	if resp, _ := call(t, srv, "GET", "/api/requests", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no cookie: got %d", resp.StatusCode)
	}
	if resp, _ := call(t, srv, "GET", "/api/requests", "session=nobody", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad cookie: got %d", resp.StatusCode)
	}
	if resp, body := call(t, srv, "GET", "/api/me", "session=alice", ""); resp.StatusCode != 200 || !strings.Contains(body, `"alice"`) {
		t.Fatalf("me: %d %s", resp.StatusCode, body)
	}
}

func TestOwnershipAndCancel(t *testing.T) {
	srv, db := setup(t)
	ctx := context.Background()
	mine, err := db.Create(ctx, store.Request{UserID: 1, Username: "alice", IGDBID: 1, Title: "Golden Sun", PlatformSlug: "gba", PlatformName: "Game Boy Advance"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetState(ctx, mine.ID, store.StateDownloading, ""); err != nil {
		t.Fatal(err)
	}
	// Bob sees an empty list and cannot touch Alice's request.
	if _, body := call(t, srv, "GET", "/api/requests", "session=bob", ""); strings.TrimSpace(body) != "[]" {
		t.Fatalf("bob list: %s", body)
	}
	if resp, _ := call(t, srv, "POST", "/api/requests/1/cancel", "session=bob", ""); resp.StatusCode != 404 {
		t.Fatalf("bob cancel: %d", resp.StatusCode)
	}
	// Alice cancels her own request even while it downloads.
	if resp, body := call(t, srv, "POST", "/api/requests/1/cancel", "session=alice", ""); resp.StatusCode != 200 || !strings.Contains(body, `"cancelled"`) {
		t.Fatalf("alice cancel: %d %s", resp.StatusCode, body)
	}
	if resp, _ := call(t, srv, "POST", "/api/requests/1/cancel", "session=alice", ""); resp.StatusCode != 409 {
		t.Fatalf("second cancel: %d", resp.StatusCode)
	}
	// Only admins delete, and admins see everyone's with ?all=1.
	if resp, _ := call(t, srv, "DELETE", "/api/requests/1", "session=alice", ""); resp.StatusCode != 403 {
		t.Fatalf("alice delete: %d", resp.StatusCode)
	}
	if _, body := call(t, srv, "GET", "/api/requests?all=1", "session=root", ""); !strings.Contains(body, "Golden Sun") {
		t.Fatalf("admin list: %s", body)
	}
	if resp, _ := call(t, srv, "DELETE", "/api/requests/1", "session=root", ""); resp.StatusCode != 204 {
		t.Fatalf("admin delete: %d", resp.StatusCode)
	}
}

func TestCreateValidation(t *testing.T) {
	srv, _ := setup(t)
	cases := map[string]string{
		`{}`:                                   "missing fields",
		`{"igdb_id":1,"platform_slug":"snes"}`: "platform not in library",
		`{"igdb_id":1,"platform_slug":"gba","extras":["cheats"]}`: "unknown extra",
	}
	for body, why := range cases {
		if resp, _ := call(t, srv, "POST", "/api/requests", "session=alice", body); resp.StatusCode != 400 {
			t.Errorf("%s: got %d", why, resp.StatusCode)
		}
	}
}

func TestEventsAreScopedToOwner(t *testing.T) {
	srv, db := setup(t)
	stream := func(cookie string) *bufio.Reader {
		req, _ := http.NewRequest("GET", srv.URL+"/ext/requests/api/events", nil)
		req.Header.Set("Cookie", cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return bufio.NewReader(resp.Body)
	}
	next := func(r *bufio.Reader) string {
		done := make(chan string, 1)
		go func() {
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					done <- ""
					return
				}
				if strings.HasPrefix(line, "data: ") {
					done <- line
					return
				}
			}
		}()
		select {
		case line := <-done:
			return line
		case <-time.After(time.Second):
			return ""
		}
	}
	alice, bob, admin := stream("session=alice"), stream("session=bob"), stream("session=root")
	time.Sleep(100 * time.Millisecond)

	req, _ := db.Create(context.Background(), store.Request{UserID: 1, Username: "alice", IGDBID: 1, Title: "Golden Sun", PlatformSlug: "gba", PlatformName: "Game Boy Advance"})
	if resp, _ := call(t, srv, "POST", "/api/requests/1/cancel", "session=alice", ""); resp.StatusCode != 200 {
		t.Fatalf("cancel %d", req.ID)
	}
	if got := next(alice); !strings.Contains(got, `"cancelled"`) {
		t.Errorf("alice should get her event, got %q", got)
	}
	if got := next(admin); !strings.Contains(got, `"cancelled"`) {
		t.Errorf("admin should get every event, got %q", got)
	}
	if got := next(bob); got != "" {
		t.Errorf("bob must not see alice's event, got %q", got)
	}
}

func TestOwnedGameIsRefusedBeforeTheIndexLoads(t *testing.T) {
	// The index has not loaded (never refreshed in tests): RomM itself must
	// be asked, and it says the game is owned.
	srv, _ := setup(t)
	resp, body := call(t, srv, "POST", "/api/requests", "session=alice", `{"igdb_id":1,"platform_slug":"gba"}`)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(body, `"rom_id":77`) {
		t.Fatalf("owned game: got %d %s", resp.StatusCode, body)
	}
}
