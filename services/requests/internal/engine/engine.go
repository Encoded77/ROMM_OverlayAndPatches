// Package engine drives requests: search, pick or ask, download, import,
// link to the library, re-search what was not found, and report every step
// live (SSE) and to the admin (ntfy).
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
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

const (
	tick          = 2 * time.Second
	resultTTL     = 25 * time.Minute // TTB forgets a search result after 30
	linkTimeout   = 30 * time.Minute // waiting for RomM to list an imported game
	maxCandidates = 15
)

// Progress is live download state, kept in memory and pushed over SSE.
type Progress struct {
	ID       int64   `json:"id"`
	Progress float64 `json:"progress"`
	Speed    int64   `json:"speed"`
	ETA      int     `json:"eta"`
	Step     string  `json:"step,omitempty"`
}

type Engine struct {
	Store    *store.Store
	Hub      *events.Hub
	Romm     *romm.Client
	Settings *settings.Holder
	Notify   *notify.Ntfy
	Importer *importer.Importer
	Index    *romm.Index
	Log      *slog.Logger
	// NewProvider builds the provider from the current settings (they can
	// change at run time).
	NewProvider func(ttb.Config) provider.Provider

	mu       sync.Mutex
	progress map[int64]Progress
	busy     map[int64]bool // a search or an import is running for it
	wake     chan struct{}
}

func (e *Engine) init() {
	e.progress = map[int64]Progress{}
	e.busy = map[int64]bool{}
	e.wake = make(chan struct{}, 1)
}

// Wake makes the loop run now (a new request, a choice).
func (e *Engine) Wake() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Progress returns the live progress of a request, if any.
func (e *Engine) ProgressOf(id int64) (Progress, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p, ok := e.progress[id]
	return p, ok
}

func (e *Engine) provider() provider.Provider { return e.NewProvider(e.Settings.Get().TTB) }

// Run loops until ctx ends. Requests interrupted by a restart resume: a
// search re-runs, a download is followed again, an import restarts.
func (e *Engine) Run(ctx context.Context) {
	e.init()
	// Searches and imports interrupted by a restart start over.
	for _, st := range []string{store.StateSearching} {
		list, _ := e.Store.ByStates(ctx, st)
		for _, r := range list {
			e.Store.Patch(ctx, r.ID, map[string]any{"state": store.StateRequested})
		}
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	slow := 0
	for {
		e.step(ctx, slow == 0)
		slow = (slow + 1) % 15 // the slow part every 30 s
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-e.wake:
			slow = 0
		}
	}
}

func (e *Engine) step(ctx context.Context, slow bool) {
	// New requests, and due re-searches.
	if list, err := e.Store.ByStates(ctx, store.StateRequested); err == nil {
		for _, r := range list {
			e.goSearch(ctx, r)
		}
	}
	if slow {
		now := time.Now().UTC().Format(time.RFC3339)
		if list, err := e.Store.ByStates(ctx, store.StateWanted, store.StateNeedsChoice); err == nil {
			for _, r := range list {
				if r.NextSearchAt != "" && r.NextSearchAt <= now {
					e.goSearch(ctx, r)
				}
			}
		}
	}
	e.followJobs(ctx)
	if list, err := e.Store.ByStates(ctx, store.StateImporting); err == nil {
		for _, r := range list {
			if r.Placed == "" {
				e.goImport(ctx, r)
			} else if slow {
				e.link(ctx, r)
			}
		}
	}
}

func (e *Engine) claim(id int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.busy[id] {
		return false
	}
	e.busy[id] = true
	return true
}

func (e *Engine) release(id int64) {
	e.mu.Lock()
	delete(e.busy, id)
	delete(e.progress, id)
	e.mu.Unlock()
}

// publish pushes a request's new state to its owner's tabs (and admins').
func (e *Engine) publish(r store.Request) {
	e.Hub.Publish(events.Event{Type: "request.updated", OwnerID: r.UserID, Data: r})
}

func (e *Engine) set(ctx context.Context, id int64, fields map[string]any) (store.Request, error) {
	r, err := e.Store.Patch(ctx, id, fields)
	if err == nil {
		e.publish(r)
	}
	return r, err
}

func (e *Engine) live(r store.Request, p Progress) {
	p.ID = r.ID
	e.mu.Lock()
	e.progress[r.ID] = p
	e.mu.Unlock()
	e.Hub.Publish(events.Event{Type: "request.progress", OwnerID: r.UserID, Data: p})
}

// ── Search ────────────────────────────────────────────────────────────

func (e *Engine) goSearch(ctx context.Context, r store.Request) {
	if !e.claim(r.ID) {
		return
	}
	go func() {
		defer e.release(r.ID)
		if err := e.search(ctx, r); err != nil {
			if ctx.Err() != nil {
				return // stopping: Run resets "searching" at the next start
			}
			e.Log.Warn("search", "id", r.ID, "err", err)
			e.schedule(ctx, r, store.StateWanted, detail("search_failed", err.Error()), nil)
		}
	}()
}

func (e *Engine) wanted(ctx context.Context, r store.Request) (match.Wanted, error) {
	plats, err := e.Romm.Platforms(ctx)
	if err != nil {
		return match.Wanted{}, err
	}
	lib := make([]match.PlatformInfo, 0, len(plats))
	for _, p := range plats {
		lib = append(lib, match.PlatformInfo{Slug: p.FSSlug, Name: p.Name})
	}
	s := e.Settings.Get()
	return match.Wanted{Title: r.Title, AltNames: r.AltNames, Platform: r.PlatformSlug,
		Rules: match.For(s.Rules, r.PlatformSlug), Library: lib}, nil
}

// Query is what is searched for a request: its title, or for an extra the
// title plus the extra's words.
func query(r store.Request) string {
	switch r.Extra {
	case "updates":
		return r.Title + " update"
	case "dlc":
		return r.Title + " dlc"
	case "translation":
		return r.Title + " traduction"
	case "hacks":
		return r.Title + " hack"
	}
	return r.Title
}

func (e *Engine) search(ctx context.Context, r store.Request) error {
	// Never fetch a game the library already has (added by hand meanwhile,
	// or requested while the index was still loading).
	if r.ParentID == nil && e.Index != nil && e.Index.Ready() {
		if romID := e.Index.Owned(r.IGDBID)[r.PlatformSlug]; romID != 0 {
			_, err := e.set(ctx, r.ID, map[string]any{"state": store.StateAvailable, "rom_id": romID,
				"detail": detail("already_owned"), "next_search_at": ""})
			return err
		}
	}
	r, err := e.set(ctx, r.ID, map[string]any{"state": store.StateSearching, "detail": ""})
	if err != nil {
		return err
	}
	w, err := e.wanted(ctx, r)
	if err != nil {
		return err
	}
	p := e.provider()
	// Search under each useful name until something is certain: the title
	// as IGDB has it, its short form, then English, unaccented and French
	// names (match.Queries). Results of every query are merged.
	queries := []string{query(r)}
	if r.Extra == "" {
		queries = match.Queries(r.Title, r.AltNames, 4)
	}
	var releases []match.Release
	var indexerErrors []string
	seen := map[string]bool{}
	var ranked []match.Scored
	for i, q := range queries {
		found, errs, err := p.Search(ctx, q)
		if err != nil {
			if i == 0 {
				return err
			}
			continue
		}
		if i == 0 {
			indexerErrors = errs
		}
		for _, rel := range found {
			// Each search hands out new result ids: the same torrent found by
			// two queries is one candidate.
			key := rel.InfoHash
			if key == "" {
				key = fmt.Sprintf("%s|%d|%s", rel.Title, rel.Size, rel.Indexer)
			}
			if !seen[key] {
				seen[key] = true
				releases = append(releases, rel)
			}
		}
		ranked = match.Rank(releases, w, false)
		if len(ranked) > 0 && ranked[0].Verdict == match.Auto {
			break
		}
	}
	// An extra is never the main game: keep what mentions the extra only.
	if r.Extra != "" {
		ranked = slices.DeleteFunc(ranked, func(s match.Scored) bool { return s.Verdict == match.Auto && !mentionsExtra(s.Title, r.Extra) })
	}
	if len(ranked) > maxCandidates {
		ranked = ranked[:maxCandidates]
	}
	cands, _ := json.Marshal(ranked)
	searched := map[string]any{"candidates": json.RawMessage(cands), "searched_at": time.Now().UTC().Format(time.RFC3339),
		"searches": r.Searches + 1, "provider": p.ID()}

	if len(ranked) > 0 && ranked[0].Verdict == match.Auto && e.Settings.Get().AutoDownload {
		if _, err := e.Store.Patch(ctx, r.ID, searched); err != nil {
			return err
		}
		return e.start(ctx, r.ID, ranked[0])
	}
	if len(ranked) > 0 {
		r, err = e.schedule(ctx, r, store.StateNeedsChoice, detail("candidates", strconv.Itoa(len(ranked))), searched)
		if err == nil && r.Searches == 1 && e.Settings.Get().Notify.NeedsChoice {
			e.Notify.Send(ctx, "Demande : choix requis", fmt.Sprintf("**%s** (%s) : %d résultat(s), aucun sûr. À choisir dans Demandes.",
				r.Title, r.PlatformName, len(ranked)), 3, "mag")
		}
		return err
	}
	why := detail("nothing_found")
	if len(indexerErrors) > 0 {
		names := make([]string, 0, len(indexerErrors))
		for _, e := range indexerErrors {
			names = append(names, strings.SplitN(e, ":", 2)[0])
		}
		why = detail("nothing_found_indexers", strings.Join(names, ", "))
	}
	_, err = e.schedule(ctx, r, store.StateWanted, why, searched)
	return err
}

func mentionsExtra(title, extra string) bool {
	t := strings.ToLower(title)
	words := map[string][]string{"updates": {"update", "patch"}, "dlc": {"dlc"}, "translation": {"translation", "traduction", "translated", "fr patch"}, "hacks": {"hack"}}[extra]
	return slices.ContainsFunc(words, func(w string) bool { return strings.Contains(t, w) })
}

// schedule parks a request until its next automatic search.
func (e *Engine) schedule(ctx context.Context, r store.Request, state, detail string, extra map[string]any) (store.Request, error) {
	delays := e.Settings.Get().ResearchMinutes
	n := r.Searches
	if v, ok := extra["searches"].(int); ok {
		n = v
	}
	delay := delays[min(max(n-1, 0), len(delays)-1)]
	fields := map[string]any{"state": state, "detail": detail,
		"next_search_at": time.Now().UTC().Add(time.Duration(delay) * time.Minute).Format(time.RFC3339)}
	for k, v := range extra {
		fields[k] = v
	}
	return e.set(ctx, r.ID, fields)
}

// ── Download ──────────────────────────────────────────────────────────

func (e *Engine) start(ctx context.Context, id int64, pick match.Scored) error {
	subdir := fmt.Sprintf("request-%d", id)
	prov := e.provider()
	dest := provider.Destination{Subdir: subdir}
	// The same torrent may already be in TTB (a previous attempt, or added by
	// hand): reuse it rather than downloading it twice.
	jobID := ""
	if ex, found, err := prov.Find(ctx, pick.InfoHash, dest); err == nil && found {
		if adopted, ok, err := prov.Adopt(ctx, ex, dest); err == nil && ok {
			jobID = adopted
			e.Log.Info("reusing download", "id", id, "job", adopted)
		}
	}
	if jobID == "" {
		if err := e.Importer.Clean(subdir); err != nil {
			return err
		}
		started, err := prov.Start(ctx, pick.Release, dest)
		if err != nil {
			return err
		}
		jobID = started
	}
	chosen, _ := json.Marshal(pick)
	r, err := e.set(ctx, id, map[string]any{"state": store.StateDownloading, "job_id": jobID,
		"chosen": json.RawMessage(chosen), "detail": detail("release", pick.Title), "next_search_at": ""})
	if err == nil {
		e.Log.Info("download started", "id", id, "release", pick.Title)
		e.live(r, Progress{})
	}
	return err
}

// Choose starts a candidate picked by a person. The result id expires in TTB
// after 30 minutes, so an older one is found again by a fresh search.
func (e *Engine) Choose(ctx context.Context, r store.Request, candidateID string) error {
	var cands []match.Scored
	_ = json.Unmarshal(r.Candidates, &cands)
	i := slices.IndexFunc(cands, func(c match.Scored) bool { return c.ID == candidateID })
	if i < 0 {
		return errors.New("unknown candidate")
	}
	pick := cands[i]
	fresh, err := time.Parse(time.RFC3339, r.SearchedAt)
	if err == nil && time.Since(fresh) < resultTTL {
		err = e.start(ctx, r.ID, pick)
		// TTB forgets its results on a restart too, not only after 30 min.
		if err == nil || !strings.Contains(err.Error(), "expired result") {
			return err
		}
	}
	// Find the same release again with a fresh search: same info hash, else
	// same title and size on the same indexer.
	queries := []string{query(r)}
	if r.Extra == "" {
		queries = match.Queries(r.Title, r.AltNames, 4)
	}
	for _, q := range queries {
		releases, _, err := e.provider().Search(ctx, q)
		if err != nil {
			continue
		}
		j := slices.IndexFunc(releases, func(x match.Release) bool {
			if pick.InfoHash != "" && x.InfoHash != "" {
				return x.InfoHash == pick.InfoHash
			}
			return x.Title == pick.Title && x.Size == pick.Size && x.Indexer == pick.Indexer
		})
		if j >= 0 {
			pick.Release = releases[j]
			return e.start(ctx, r.ID, pick)
		}
	}
	return errors.New("that result is no longer offered, search again")
}

// followJobs polls the provider for every running download.
func (e *Engine) followJobs(ctx context.Context) {
	list, err := e.Store.ByStates(ctx, store.StateDownloading, store.StateScanning)
	if err != nil || len(list) == 0 {
		return
	}
	ids := make([]string, 0, len(list))
	for _, r := range list {
		ids = append(ids, r.JobID)
	}
	status, err := e.provider().Status(ctx, ids)
	if err != nil {
		e.Log.Warn("job status", "err", err)
		return
	}
	for _, r := range list {
		st, ok := status[r.JobID]
		if !ok {
			e.fail(ctx, r, detail("job_gone"))
			continue
		}
		switch st.State {
		case provider.JobQueued, provider.JobDownloading:
			if r.State != store.StateDownloading {
				r, _ = e.set(ctx, r.ID, map[string]any{"state": store.StateDownloading})
			}
			e.live(r, Progress{Progress: st.Progress, Speed: st.Speed, ETA: st.ETA})
		case provider.JobScanning:
			if r.State != store.StateScanning {
				r, _ = e.set(ctx, r.ID, map[string]any{"state": store.StateScanning, "detail": ""})
			}
			e.live(r, Progress{Progress: 1, Step: "scan"})
		case provider.JobDone:
			e.set(ctx, r.ID, map[string]any{"state": store.StateImporting, "detail": "", "progress": nil})
			e.Wake()
		case provider.JobInfected:
			e.fail(ctx, r, detail("malware"))
		case provider.JobCancelled:
			e.fail(ctx, r, detail("job_cancelled"))
		default:
			e.fail(ctx, r, detail("download_failed", st.Error))
		}
	}
}

func (e *Engine) fail(ctx context.Context, r store.Request, why string) {
	r, err := e.set(ctx, r.ID, map[string]any{"state": store.StateFailed, "detail": why})
	if err != nil {
		return
	}
	e.Log.Warn("request failed", "id", r.ID, "detail", why)
	e.release(r.ID)
	if e.Settings.Get().Notify.Failed {
		e.Notify.Send(ctx, "Demande en échec", fmt.Sprintf("**%s** (%s) : %s", r.Title, r.PlatformName, french(why)), 4, "warning")
	}
}

// Cancel stops a request at any stage, a failed one included; its download
// is cancelled and what it delivered is deleted.
func (e *Engine) Cancel(ctx context.Context, r store.Request, by string) (store.Request, error) {
	defer e.release(r.ID)
	e.Discard(ctx, r)
	return e.set(ctx, r.ID, map[string]any{"state": store.StateCancelled, "detail": detail("cancelled_by", by), "next_search_at": ""})
}

// Discard drops a request's provider job and staging files (before a cancel
// or a delete). A game already placed in the library stays.
func (e *Engine) Discard(ctx context.Context, r store.Request) {
	if r.JobID != "" && r.State != store.StateAvailable && r.State != store.StateCancelled {
		if err := e.provider().Cancel(ctx, r.JobID, true); err != nil {
			e.Log.Warn("cancel job", "id", r.ID, "err", err)
		}
	}
	if r.Placed == "" {
		_ = e.Importer.Clean(fmt.Sprintf("request-%d", r.ID))
	}
}

// Retry resumes a failed or cancelled request: a download that finished is
// imported again, one still running is followed, anything else starts over.
func (e *Engine) Retry(ctx context.Context, r store.Request) (store.Request, error) {
	if r.JobID != "" {
		if st, err := e.provider().Status(ctx, []string{r.JobID}); err == nil {
			switch st[r.JobID].State {
			case provider.JobDone:
				e.Wake()
				return e.set(ctx, r.ID, map[string]any{"state": store.StateImporting, "detail": "", "placed": ""})
			case provider.JobQueued, provider.JobDownloading, provider.JobScanning:
				return e.set(ctx, r.ID, map[string]any{"state": store.StateDownloading, "detail": ""})
			}
		}
	}
	r, err := e.set(ctx, r.ID, map[string]any{"state": store.StateRequested, "detail": "", "job_id": "",
		"chosen": "", "placed": "", "searches": 0, "next_search_at": ""})
	e.Wake()
	return r, err
}

// ── Import and link ───────────────────────────────────────────────────

func (e *Engine) goImport(ctx context.Context, r store.Request) {
	if !e.claim(r.ID) {
		return
	}
	go func() {
		defer e.release(r.ID)
		rules := match.For(e.Settings.Get().Rules, r.PlatformSlug)
		job := importer.Job{
			Subdir: fmt.Sprintf("request-%d", r.ID), Platform: r.PlatformSlug, Title: r.Title,
			Formats: rules.Formats, Convert: rules.Convert, AsFolder: len(r.Extras) > 0,
			Pick: func(names []string) int { return match.BestFile(names, rules) },
			Step: func(s string) { e.live(r, Progress{Progress: 1, Step: s}) },
		}
		if r.ParentID != nil {
			parent, err := e.Store.Get(ctx, *r.ParentID)
			if err != nil || parent.Placed == "" {
				e.fail(ctx, r, detail("parent_missing"))
				return
			}
			// A game owned as a single file is wrapped in a folder first.
			folder, err := e.Importer.EnsureFolder(parent.Placed)
			if err != nil {
				e.fail(ctx, r, detail("parent_missing"))
				return
			}
			if folder != parent.Placed {
				e.set(ctx, parent.ID, map[string]any{"placed": folder})
			}
			job.Into = folder + "/" + extraFolder[r.Extra]
		}
		res, err := e.Importer.Run(ctx, job)
		// The service is stopping: the import resumes at the next start.
		if ctx.Err() != nil {
			return
		}
		// Cancelled while importing: stop here, keep what was placed.
		if cur, err2 := e.Store.Get(ctx, r.ID); err2 == nil && cur.State == store.StateCancelled {
			return
		}
		if errors.Is(err, importer.ErrNoDelivery) {
			// Nothing to import from: forget that job and start the request
			// over (a cached torrent comes back in seconds).
			_ = e.provider().Cancel(ctx, r.JobID, false)
			e.Log.Warn("delivery missing, starting over", "id", r.ID, "job", r.JobID)
			e.set(ctx, r.ID, map[string]any{"state": store.StateRequested, "job_id": "", "chosen": "",
				"detail": "", "next_search_at": ""})
			e.Wake()
			return
		}
		if err != nil && res.Placed == "" {
			e.fail(ctx, r, detail("import_failed", err.Error()))
			return
		}
		if err != nil {
			e.Log.Warn("import", "id", r.ID, "err", err)
		}
		// The provider's job has served its purpose.
		_ = e.provider().Cancel(ctx, r.JobID, false)
		e.set(ctx, r.ID, map[string]any{"placed": res.Placed, "detail": detail("waiting_scan", res.Placed)})
	}()
}

// extraFolder: RomM's file category folders inside a game folder.
var extraFolder = map[string]string{"updates": "update", "dlc": "dlc", "translation": "translation", "hacks": "hack"}

// link marks an imported request available once RomM lists the game.
func (e *Engine) link(ctx context.Context, r store.Request) {
	// An extra is part of its game: available once RomM lists the game's
	// folder again (a wrapped single file becomes a new path to rescan).
	if r.ParentID != nil {
		parent, err := e.Store.Get(ctx, *r.ParentID)
		if err != nil {
			return
		}
		plats, err := e.Romm.Platforms(ctx)
		if err != nil {
			return
		}
		i := slices.IndexFunc(plats, func(p romm.Platform) bool { return p.FSSlug == parent.PlatformSlug })
		if i < 0 {
			return
		}
		folder := parent.Placed[strings.LastIndexByte(parent.Placed, '/')+1:]
		rom, err := e.Romm.FindRom(ctx, plats[i].ID, folder)
		if err != nil || rom.ID == 0 {
			if time.Since(r.UpdatedAt) > linkTimeout {
				e.fail(ctx, r, detail("not_listed", parent.Placed))
			}
			return
		}
		if parent.RomID == nil || *parent.RomID != rom.ID {
			e.set(ctx, parent.ID, map[string]any{"rom_id": rom.ID})
		}
		e.set(ctx, r.ID, map[string]any{"state": store.StateAvailable, "rom_id": rom.ID, "detail": ""})
		return
	}
	plats, err := e.Romm.Platforms(ctx)
	if err != nil {
		return
	}
	i := slices.IndexFunc(plats, func(p romm.Platform) bool { return p.FSSlug == r.PlatformSlug })
	if i < 0 {
		return
	}
	fsName := r.Placed[strings.IndexByte(r.Placed, '/')+1:]
	rom, err := e.Romm.FindRom(ctx, plats[i].ID, fsName)
	if err != nil {
		return
	}
	if rom.ID == 0 {
		if time.Since(r.UpdatedAt) > linkTimeout {
			e.fail(ctx, r, detail("not_listed", r.Placed))
		}
		return
	}
	// The request knows exactly which game this is; RomM's own scan may not
	// (a release named "Ico. PS2.iso" matched nothing).
	if rom.IGDBID == nil {
		if err := e.Romm.MatchIGDB(ctx, rom.ID, r.IGDBID); err != nil {
			e.Log.Warn("match igdb", "rom", rom.ID, "err", err)
		}
	}
	r, err = e.set(ctx, r.ID, map[string]any{"state": store.StateAvailable, "rom_id": rom.ID, "detail": ""})
	if err != nil {
		return
	}
	e.Log.Info("available", "id", r.ID, "rom", rom.ID)
	if e.Index != nil {
		e.Index.Add(r.IGDBID, r.PlatformSlug, rom.ID)
	}
	if e.Settings.Get().Notify.Available {
		e.Notify.Send(ctx, "Jeu disponible", fmt.Sprintf("**%s** (%s), demandé par %s, est dans la ludothèque.",
			r.Title, r.PlatformName, r.Username), 3, "video_game")
	}
	e.spawnExtras(ctx, r)
}

// SpawnExtras creates the child requests for extras once the game is in.
func (e *Engine) SpawnExtras(ctx context.Context, r store.Request) { e.spawnExtras(ctx, r) }

// spawnExtras creates the child requests for extras once the game is in.
func (e *Engine) spawnExtras(ctx context.Context, r store.Request) {
	for _, extra := range r.Extras {
		child := r
		child.ParentID = &r.ID
		child.Extra = extra
		child.Extras = []string{}
		if c, err := e.Store.Create(ctx, child); err == nil {
			e.publish(c)
		}
	}
	if len(r.Extras) > 0 {
		e.Wake()
	}
}

// Test ranks a live search for a title on a platform, rejected results
// included, for the settings page. Nothing is started or stored.
func (e *Engine) Test(ctx context.Context, title, platform string) ([]match.Scored, []string, error) {
	w, err := e.wanted(ctx, store.Request{Title: title, PlatformSlug: platform})
	if err != nil {
		return nil, nil, err
	}
	// The same queries the engine would run (normalised, then short form).
	var releases []match.Release
	var errs []string
	seen := map[string]bool{}
	for i, q := range match.Queries(title, nil, 2) {
		found, qerrs, err := e.provider().Search(ctx, q)
		if err != nil {
			if i == 0 {
				return nil, nil, err
			}
			continue
		}
		if i == 0 {
			errs = qerrs
		}
		for _, rel := range found {
			if !seen[rel.ID] {
				seen[rel.ID] = true
				releases = append(releases, rel)
			}
		}
	}
	ranked := match.Rank(releases, w, true)
	if ranked == nil {
		ranked = []match.Scored{}
	}
	return ranked, errs, nil
}

// detail encodes a request's explanation as "code|argument". The page
// translates the code (ext-requests.detail.<code>); the argument is data
// (a release title, indexer names, an error message).
func detail(code string, arg ...string) string {
	if len(arg) == 0 {
		return code
	}
	return code + "|" + strings.Join(arg, " ")
}

// french renders a detail for the admin's ntfy messages.
func french(d string) string {
	code, arg, _ := strings.Cut(d, "|")
	text := map[string]string{
		"job_gone":        "le téléchargement a disparu de TTB",
		"malware":         "malware détecté, téléchargement mis en quarantaine",
		"job_cancelled":   "téléchargement annulé dans TTB",
		"download_failed": "échec du téléchargement",
		"parent_missing":  "le jeu de cet extra n'est pas dans la ludothèque",
		"import_failed":   "échec de l'import",
		"not_listed":      "placé mais RomM ne le liste pas (scanner à la main)",
	}[code]
	if text == "" {
		text = code
	}
	if arg != "" {
		text += " : " + arg
	}
	return text
}
