package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/events"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/importer"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/match"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/notify"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/provider"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/provider/ttb"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/romm"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/settings"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/store"
)

// fakeProvider delivers a file into staging when a job "finishes".
type fakeProvider struct {
	existing  *provider.Existing // what Find reports
	started   int                // Start calls
	mu        sync.Mutex
	releases  []match.Release
	staging   string
	jobs      map[string]string // job id -> state
	cancelled []string
}

func (f *fakeProvider) ID() string { return "fake" }
func (f *fakeProvider) Search(context.Context, string) ([]match.Release, []string, error) {
	return f.releases, nil, nil
}
func (f *fakeProvider) Start(_ context.Context, r match.Release, d provider.Destination) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started++
	id := "job-" + d.Subdir
	f.jobs[id] = provider.JobDownloading
	os.MkdirAll(filepath.Join(f.staging, d.Subdir), 0o755)
	os.WriteFile(filepath.Join(f.staging, d.Subdir, r.Title+".gba"), []byte("rom"), 0o644)
	return id, nil
}
func (f *fakeProvider) Status(_ context.Context, ids []string) (map[string]provider.JobStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]provider.JobStatus{}
	for _, id := range ids {
		if st, ok := f.jobs[id]; ok {
			out[id] = provider.JobStatus{State: st, Progress: 0.5}
		}
	}
	return out, nil
}
func (f *fakeProvider) Cancel(_ context.Context, id string, purge bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if purge {
		f.cancelled = append(f.cancelled, id)
	}
	delete(f.jobs, id)
	return nil
}
func (f *fakeProvider) Find(context.Context, string, provider.Destination) (provider.Existing, bool, error) {
	if f.existing == nil {
		return provider.Existing{}, false, nil
	}
	return *f.existing, true, nil
}
func (f *fakeProvider) Adopt(_ context.Context, ex provider.Existing, d provider.Destination) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[ex.ID] = ex.State
	return ex.ID, true, nil
}
func (f *fakeProvider) finish(id string) {
	f.mu.Lock()
	f.jobs[id] = provider.JobDone
	f.mu.Unlock()
}

// fakeRomm lists one platform and finds a rom once "scanned".
func fakeRomm(t *testing.T, scanned *bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/platforms":
			gba := 24
			json.NewEncoder(w).Encode([]romm.Platform{{ID: 2, Slug: "gba", FSSlug: "gba", Name: "Game Boy Advance", IGDBID: &gba}})
		case "/api/roms":
			items := []romm.Rom{}
			if *scanned {
				items = append(items, romm.Rom{ID: 900, FSName: "Golden Sun (Europe) GBA.gba"})
			}
			json.NewEncoder(w).Encode(map[string]any{"items": items})
		default:
			http.NotFound(w, r)
		}
	}))
}

func setup(t *testing.T, releases []match.Release) (*Engine, *fakeProvider, *bool, string) {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	scanned := new(bool)
	srv := fakeRomm(t, scanned)
	t.Cleanup(srv.Close)
	held, _ := settings.Load(context.Background(), db, settings.Defaults("", ""))
	fp := &fakeProvider{releases: releases, staging: filepath.Join(root, "staging"), jobs: map[string]string{}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := &Engine{Store: db, Hub: events.NewHub(), Romm: romm.New(srv.URL, "t", time.Minute), Settings: held,
		Notify: notify.New("", "", "", log), Log: log,
		Importer:    importer.New(fp.staging, filepath.Join(root, "lib"), ""),
		NewProvider: func(ttb.Config) provider.Provider { return fp }}
	e.init()
	return e, fp, scanned, root
}

func waitState(t *testing.T, e *Engine, id int64, want string) store.Request {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		e.step(context.Background(), true)
		r, _ := e.Store.Get(context.Background(), id)
		if r.State == want {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	r, _ := e.Store.Get(context.Background(), id)
	t.Fatalf("request %d stuck in %s (%s), want %s", id, r.State, r.Detail, want)
	return r
}

// eventually steps the engine (fast part only) until cond holds. Searches and
// imports run in goroutines, so a fixed sleep races them on a busy machine.
func eventually(t *testing.T, e *Engine, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		e.step(context.Background(), false)
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func newRequest(t *testing.T, e *Engine, extras ...string) store.Request {
	r, err := e.Store.Create(context.Background(), store.Request{UserID: 1, Username: "alice", IGDBID: 7,
		Title: "Golden Sun", PlatformSlug: "gba", PlatformName: "Game Boy Advance", Extras: extras})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestConfidentResultGoesAllTheWay(t *testing.T) {
	e, fp, scanned, root := setup(t, []match.Release{{ID: "r1", Title: "Golden Sun (Europe) GBA", Size: 8 << 20, Seeders: 5, Usable: true}})
	r := newRequest(t, e)
	r = waitState(t, e, r.ID, store.StateDownloading)
	// The search goroutine clears the progress when it ends; the next step sets it again.
	eventually(t, e, "live progress", func() bool { p, ok := e.ProgressOf(r.ID); return ok && p.Progress == 0.5 })
	fp.finish(r.JobID)
	r = waitState(t, e, r.ID, store.StateImporting)
	rom := filepath.Join(root, "lib", "gba", "Golden Sun (Europe) GBA.gba")
	eventually(t, e, "the rom placed", func() bool { _, err := os.Stat(rom); return err == nil })
	*scanned = true
	r = waitState(t, e, r.ID, store.StateAvailable)
	if r.RomID == nil || *r.RomID != 900 {
		t.Fatalf("rom not linked: %+v", r.RomID)
	}
}

func TestNothingFoundIsWantedWithBackoff(t *testing.T) {
	e, _, _, _ := setup(t, nil)
	r := newRequest(t, e)
	r = waitState(t, e, r.ID, store.StateWanted)
	next, err := time.Parse(time.RFC3339, r.NextSearchAt)
	if err != nil || time.Until(next) < 14*time.Minute {
		t.Fatalf("next search should be ~15 min away: %q", r.NextSearchAt)
	}
}

func TestUncertainNeedsChoiceThenChoose(t *testing.T) {
	e, _, _, _ := setup(t, []match.Release{{ID: "r2", Title: "Golden Sun 2 GBA", Size: 8 << 20, Seeders: 5, Usable: true}})
	r := newRequest(t, e)
	r = waitState(t, e, r.ID, store.StateNeedsChoice)
	if err := e.Choose(context.Background(), r, "r2"); err != nil {
		t.Fatal(err)
	}
	waitState(t, e, r.ID, store.StateDownloading)
}

func TestCancelDuringDownloadPurgesJob(t *testing.T) {
	e, fp, _, _ := setup(t, []match.Release{{ID: "r1", Title: "Golden Sun (Europe) GBA", Size: 8 << 20, Seeders: 5, Usable: true}})
	r := newRequest(t, e)
	r = waitState(t, e, r.ID, store.StateDownloading)
	r, err := e.Cancel(context.Background(), r, "alice")
	if err != nil || r.State != store.StateCancelled {
		t.Fatalf("cancel: %v %s", err, r.State)
	}
	if len(fp.cancelled) != 1 {
		t.Fatalf("job should be purged, got %v", fp.cancelled)
	}
}

func TestExtrasFollowTheGame(t *testing.T) {
	e, fp, scanned, root := setup(t, []match.Release{
		{ID: "r1", Title: "Golden Sun (Europe) GBA", Size: 8 << 20, Seeders: 5, Usable: true},
	})
	r := newRequest(t, e, "translation")
	r = waitState(t, e, r.ID, store.StateDownloading)
	fp.finish(r.JobID)
	waitState(t, e, r.ID, store.StateImporting)
	// With an extra requested, the game lands in its own folder.
	eventually(t, e, "the game placed", func() bool { r, _ = e.Store.Get(context.Background(), r.ID); return r.Placed != "" })
	if r.Placed != "gba/Golden Sun" {
		t.Fatalf("placed %q, want a folder", r.Placed)
	}
	*scanned = true
	// FindRom matches the folder name.
	e.Romm = romm.New(fakeFolderRomm(t).URL, "t", time.Minute)
	waitState(t, e, r.ID, store.StateAvailable)
	list, _ := e.Store.List(context.Background(), 0, false)
	var child *store.Request
	for i := range list {
		if list[i].ParentID != nil && *list[i].ParentID == r.ID {
			child = &list[i]
		}
	}
	if child == nil || child.Extra != "translation" {
		t.Fatalf("extra child request missing: %+v", list)
	}
	fp.releases = []match.Release{{ID: "t1", Title: "Golden Sun traduction FR GBA", Size: 1 << 20, Seeders: 2, Usable: true}}
	// The candidate is not exact (extra words), so it waits for a choice.
	c := waitState(t, e, child.ID, store.StateNeedsChoice)
	if err := e.Choose(context.Background(), c, "t1"); err != nil {
		t.Fatal(err)
	}
	c = waitState(t, e, child.ID, store.StateDownloading)
	fp.finish(c.JobID)
	waitState(t, e, child.ID, store.StateImporting)
	time.Sleep(100 * time.Millisecond)
	waitState(t, e, child.ID, store.StateAvailable)
	if _, err := os.Stat(filepath.Join(root, "lib", "gba", "Golden Sun", "translation")); err != nil {
		t.Fatalf("translation folder missing: %v", err)
	}
}

func fakeFolderRomm(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/platforms":
			gba := 24
			json.NewEncoder(w).Encode([]romm.Platform{{ID: 2, Slug: "gba", FSSlug: "gba", Name: "Game Boy Advance", IGDBID: &gba}})
		case "/api/roms":
			json.NewEncoder(w).Encode(map[string]any{"items": []romm.Rom{{ID: 901, FSName: "Golden Sun"}}})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestExistingDownloadIsReused(t *testing.T) {
	e, fp, _, _ := setup(t, []match.Release{{ID: "r1", Title: "Golden Sun (Europe) GBA", Size: 8 << 20, Seeders: 5, Usable: true, InfoHash: "abc"}})
	fp.existing = &provider.Existing{ID: "old-job", State: provider.JobDone, Storage: "roms-staging", Subdir: "request-1"}
	r := newRequest(t, e)
	r = waitState(t, e, r.ID, store.StateImporting)
	if fp.started != 0 || r.JobID != "old-job" {
		t.Fatalf("should reuse old-job without a new download: started=%d job=%q", fp.started, r.JobID)
	}
}

func TestRetryReimportsAFinishedDownload(t *testing.T) {
	e, fp, _, _ := setup(t, []match.Release{{ID: "r1", Title: "Golden Sun (Europe) GBA", Size: 8 << 20, Seeders: 5, Usable: true}})
	r := newRequest(t, e)
	r = waitState(t, e, r.ID, store.StateDownloading)
	fp.finish(r.JobID)
	r, _ = e.Store.Patch(context.Background(), r.ID, map[string]any{"state": store.StateFailed, "detail": "import_failed|killed"})
	r, err := e.Retry(context.Background(), r)
	if err != nil || r.State != store.StateImporting {
		t.Fatalf("retry should resume the import: %v %s", err, r.State)
	}
	if fp.started != 1 {
		t.Fatalf("retry must not download again, started=%d", fp.started)
	}
}

func TestCancelAFailedImportPurgesJobAndStaging(t *testing.T) {
	e, fp, _, _ := setup(t, []match.Release{{ID: "r1", Title: "Golden Sun (Europe) GBA", Size: 8 << 20, Seeders: 5, Usable: true}})
	r := newRequest(t, e)
	r = waitState(t, e, r.ID, store.StateDownloading)
	fp.finish(r.JobID)
	r, _ = e.Store.Patch(context.Background(), r.ID, map[string]any{"state": store.StateFailed, "detail": "import_failed|bad archive"})
	staged := filepath.Join(fp.staging, fmt.Sprintf("request-%d", r.ID))
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("staging should hold the delivery: %v", err)
	}
	r, err := e.Cancel(context.Background(), r, "alice")
	if err != nil || r.State != store.StateCancelled {
		t.Fatalf("cancel: %v %s", err, r.State)
	}
	if len(fp.cancelled) != 1 {
		t.Fatalf("finished job should be purged, got %v", fp.cancelled)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staging should be gone: %v", err)
	}
}
