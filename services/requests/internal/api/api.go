// Package api serves the requests API under the configured base path:
//
//	GET    /api/me               the RomM user behind the session cookie
//	GET    /api/platforms        library platforms a game can be requested for
//	GET    /api/search?q=        IGDB games on those platforms
//	GET    /api/requests         own requests (admins: ?all=1 for everyone's), ?history=1
//	POST   /api/requests         {igdb_id, platform_slug, extras}
//	POST   /api/requests/{id}/cancel
//	POST   /api/requests/{id}/choose   {candidate_id}
//	POST   /api/requests/{id}/retry    failed or cancelled, back to the start
//	POST   /api/requests/{id}/search   search again now
//	DELETE /api/requests/{id}    admins only
//	GET    /api/related?igdb_id= games related to one (franchise, similar), with ownership
//	GET    /api/events           SSE stream of request changes and download progress
//	GET    /api/admin/settings   PUT to save (admins)
//	POST   /api/admin/test       {title, platform}: candidates with score breakdown (admins)
//	GET    /healthz
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/engine"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/events"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/igdb"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/notify"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/romm"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/settings"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/store"
)

type Server struct {
	Romm     *romm.Client
	IGDB     *igdb.Client
	Store    *store.Store
	Hub      *events.Hub
	Log      *slog.Logger
	Engine   *engine.Engine
	Index    *romm.Index
	Settings *settings.Holder
	Notify   *notify.Ntfy
}

type ctxKey struct{}

func userFrom(ctx context.Context) romm.User { return ctx.Value(ctxKey{}).(romm.User) }

// Handler mounts every route below base.
func (s *Server) Handler(base string) http.Handler {
	mux := http.NewServeMux()
	auth := s.authenticated
	mux.HandleFunc("GET "+base+"/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.Handle("GET "+base+"/api/me", auth(s.me))
	mux.Handle("GET "+base+"/api/platforms", auth(s.platforms))
	mux.Handle("GET "+base+"/api/search", auth(s.search))
	mux.Handle("GET "+base+"/api/requests", auth(s.listRequests))
	mux.Handle("POST "+base+"/api/requests", auth(s.createRequest))
	mux.Handle("POST "+base+"/api/requests/{id}/cancel", auth(s.cancelRequest))
	mux.Handle("POST "+base+"/api/requests/{id}/choose", auth(s.chooseCandidate))
	mux.Handle("POST "+base+"/api/requests/{id}/retry", auth(s.retryRequest))
	mux.Handle("POST "+base+"/api/requests/{id}/search", auth(s.searchAgain))
	mux.Handle("DELETE "+base+"/api/requests/{id}", auth(s.deleteRequest))
	mux.Handle("GET "+base+"/api/related", auth(s.related))
	mux.Handle("GET "+base+"/api/events", auth(s.events))
	mux.Handle("GET "+base+"/api/admin/settings", auth(admin(s.getSettings)))
	mux.Handle("PUT "+base+"/api/admin/settings", auth(admin(s.putSettings)))
	mux.Handle("POST "+base+"/api/admin/test", auth(admin(s.testTitle)))
	return mux
}

// authenticated resolves the RomM user from the browser's own cookie. The
// service sits on RomM's origin, so the browser sends RomM's session along.
func (s *Server) authenticated(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.Romm.UserFromCookie(r.Context(), r.Header.Get("Cookie"))
		if errors.Is(err, romm.ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "not logged in to RomM")
			return
		}
		if err != nil {
			s.Log.Error("auth", "err", err)
			writeError(w, http.StatusBadGateway, "RomM unreachable")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, user)))
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFrom(r.Context()))
}

type platform struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	IGDBID int    `json:"igdb_id"`
	// OwnedRomID is set when the library already has this game on this platform.
	OwnedRomID int `json:"owned_rom_id,omitempty"`
	// Resembles is a library ROM on this platform with a similar title,
	// shown as a warning only (an IGDB remake or bundle entry, typically).
	Resembles *romm.Named `json:"resembles,omitempty"`
}

func admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r.Context()).IsAdmin() {
			writeError(w, http.StatusForbidden, "admins only")
			return
		}
		next(w, r)
	}
}

// requestable lists the library platforms that have an IGDB id (a request
// is matched against IGDB, so a platform without one cannot be requested).
func (s *Server) requestable(ctx context.Context) ([]platform, error) {
	plats, err := s.Romm.Platforms(ctx)
	if err != nil {
		return nil, err
	}
	out := []platform{}
	for _, p := range plats {
		if p.IGDBID != nil {
			out = append(out, platform{Slug: p.FSSlug, Name: p.Name, IGDBID: *p.IGDBID})
		}
	}
	slices.SortFunc(out, func(a, b platform) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		}
		return 0
	})
	return out, nil
}

func (s *Server) platforms(w http.ResponseWriter, r *http.Request) {
	plats, err := s.requestable(r.Context())
	if err != nil {
		s.fail(w, "platforms", err)
		return
	}
	writeJSON(w, http.StatusOK, plats)
}

type searchResult struct {
	igdb.Game
	// Platforms the game can be requested for: its IGDB platforms that the
	// library also has, as library slugs.
	Platforms []platform `json:"platforms"`
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	plats, err := s.requestable(r.Context())
	if err != nil {
		s.fail(w, "platforms", err)
		return
	}
	byIGDB := map[int]platform{}
	ids := make([]int, 0, len(plats))
	for _, p := range plats {
		byIGDB[p.IGDBID] = p
		ids = append(ids, p.IGDBID)
	}
	games, err := s.IGDB.Search(r.Context(), r.URL.Query().Get("q"), ids, 30)
	if err != nil {
		s.fail(w, "igdb search", err)
		return
	}
	// IGDB ranks by its own relevance; an exact title match goes first, so
	// "golden sun" leads with Golden Sun rather than its sequel.
	term := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	slices.SortStableFunc(games, func(a, b igdb.Game) int {
		ea, eb := strings.ToLower(a.Name) == term, strings.ToLower(b.Name) == term
		switch {
		case ea && !eb:
			return -1
		case eb && !ea:
			return 1
		}
		return 0
	})
	writeJSON(w, http.StatusOK, s.annotate(games, byIGDB))
}

// annotate keeps, per game, the library platforms it exists on, and says
// which of them already hold it.
func (s *Server) annotate(games []igdb.Game, byIGDB map[int]platform) []searchResult {
	results := make([]searchResult, 0, len(games))
	for _, g := range games {
		res := searchResult{Game: g, Platforms: []platform{}}
		owned := s.Index.Owned(g.ID)
		for _, id := range g.PlatformIDs {
			if p, ok := byIGDB[id]; ok {
				p.OwnedRomID = owned[p.Slug]
				if p.OwnedRomID == 0 {
					if n, ok := s.Index.Resembling(p.Slug, g.Name); ok {
						p.Resembles = &n
					}
				}
				res.Platforms = append(res.Platforms, p)
			}
		}
		if len(res.Platforms) > 0 {
			results = append(results, res)
		}
	}
	return results
}

func (s *Server) related(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("igdb_id"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "igdb_id is required")
		return
	}
	plats, err := s.requestable(r.Context())
	if err != nil {
		s.fail(w, "platforms", err)
		return
	}
	byIGDB := map[int]platform{}
	for _, p := range plats {
		byIGDB[p.IGDBID] = p
	}
	games, err := s.IGDB.Related(r.Context(), id)
	if err != nil {
		s.fail(w, "igdb related", err)
		return
	}
	// Only what the library lacks on at least one platform it has.
	var missing []searchResult
	for _, res := range s.annotate(games, byIGDB) {
		if slices.ContainsFunc(res.Platforms, func(p platform) bool { return p.OwnedRomID == 0 }) {
			missing = append(missing, res)
		}
	}
	if missing == nil {
		missing = []searchResult{}
	}
	writeJSON(w, http.StatusOK, missing)
}

func (s *Server) listRequests(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())
	owner := user.ID
	if user.IsAdmin() && r.URL.Query().Get("all") == "1" {
		owner = 0
	}
	list, err := s.Store.List(r.Context(), owner, r.URL.Query().Get("history") == "1")
	if err != nil {
		s.fail(w, "list", err)
		return
	}
	out := make([]listItem, len(list))
	for i, req := range list {
		out[i] = listItem{Request: req}
		if p, ok := s.Engine.ProgressOf(req.ID); ok {
			out[i].Live = &p
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type listItem struct {
	store.Request
	Live *engine.Progress `json:"live,omitempty"`
}

var allowedExtras = []string{"updates", "dlc", "translation", "hacks"}

func (s *Server) createRequest(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())
	var body struct {
		IGDBID       int      `json:"igdb_id"`
		PlatformSlug string   `json:"platform_slug"`
		Extras       []string `json:"extras"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil || body.IGDBID <= 0 || body.PlatformSlug == "" {
		writeError(w, http.StatusBadRequest, "igdb_id and platform_slug are required")
		return
	}
	for _, e := range body.Extras {
		if !slices.Contains(allowedExtras, e) {
			writeError(w, http.StatusBadRequest, "unknown extra: "+e)
			return
		}
	}

	plats, err := s.requestable(r.Context())
	if err != nil {
		s.fail(w, "platforms", err)
		return
	}
	idx := slices.IndexFunc(plats, func(p platform) bool { return p.Slug == body.PlatformSlug })
	if idx < 0 {
		writeError(w, http.StatusBadRequest, "not a library platform: "+body.PlatformSlug)
		return
	}
	plat := plats[idx]

	game, err := s.IGDB.Get(r.Context(), body.IGDBID)
	if err != nil {
		s.fail(w, "igdb get", err)
		return
	}
	if !slices.Contains(game.PlatformIDs, plat.IGDBID) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%s was not released on %s", game.Name, plat.Name))
		return
	}
	ownedRom, err := s.ownedOn(r.Context(), game, plat)
	if err != nil {
		s.fail(w, "library check", err)
		return
	}
	if ownedRom != 0 && len(body.Extras) == 0 {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "already in the library", "rom_id": ownedRom})
		return
	}
	if ownedRom != 0 {
		s.requestExtrasOnly(w, r, user, game, plat, ownedRom, body.Extras)
		return
	}
	if existing, err := s.Store.OpenFor(r.Context(), user.ID, game.ID, plat.Slug); err != nil {
		s.fail(w, "duplicate check", err)
		return
	} else if existing != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "already requested", "request": existing})
		return
	}

	req, err := s.Store.Create(r.Context(), store.Request{
		UserID: user.ID, Username: user.Username, IGDBID: game.ID, Title: game.Name, Year: game.Year,
		CoverURL: game.CoverURL, PlatformSlug: plat.Slug, PlatformName: plat.Name, Extras: body.Extras,
		AltNames: game.AltNames,
	})
	if err != nil {
		s.fail(w, "create", err)
		return
	}
	s.Log.Info("request created", "id", req.ID, "user", user.Username, "game", req.Title, "platform", req.PlatformSlug)
	s.Hub.Publish(events.Event{Type: "request.updated", OwnerID: req.UserID, Data: req})
	if s.Settings.Get().Notify.NewRequest {
		s.Notify.Send(context.WithoutCancel(r.Context()), "Nouvelle demande",
			fmt.Sprintf("**%s** (%s) demandé par %s", req.Title, req.PlatformName, req.Username), 3, "gift")
	}
	s.Engine.Wake()
	writeJSON(w, http.StatusCreated, req)
}

// ownedOn says whether the library has this game on this platform. The
// index answers once loaded; until then (the first seconds after a start)
// RomM is asked directly, so an owned game can never be downloaded again.
func (s *Server) ownedOn(ctx context.Context, game igdb.Game, plat platform) (int, error) {
	if s.Index.Ready() {
		return s.Index.Owned(game.ID)[plat.Slug], nil
	}
	plats, err := s.Romm.Platforms(ctx)
	if err != nil {
		return 0, err
	}
	i := slices.IndexFunc(plats, func(p romm.Platform) bool { return p.FSSlug == plat.Slug })
	if i < 0 {
		return 0, nil
	}
	rom, err := s.Romm.HasGame(ctx, plats[i].ID, game.ID, game.Name)
	return rom.ID, err
}

// requestExtrasOnly handles extras for a game the library already has: the
// game itself is recorded as available (nothing to download) and each extra
// becomes a child request that downloads into the game's folder.
func (s *Server) requestExtrasOnly(w http.ResponseWriter, r *http.Request, user romm.User, game igdb.Game, plat platform, romID int, extras []string) {
	rom, err := s.Romm.GetRom(r.Context(), romID)
	if err != nil {
		s.fail(w, "rom", err)
		return
	}
	req, err := s.Store.Create(r.Context(), store.Request{
		UserID: user.ID, Username: user.Username, IGDBID: game.ID, Title: game.Name, Year: game.Year,
		CoverURL: game.CoverURL, PlatformSlug: plat.Slug, PlatformName: plat.Name, Extras: extras,
		AltNames: game.AltNames,
	})
	if err != nil {
		s.fail(w, "create", err)
		return
	}
	req, err = s.Store.Patch(r.Context(), req.ID, map[string]any{
		"state": store.StateAvailable, "rom_id": romID, "placed": plat.Slug + "/" + rom.FSName,
	})
	if err != nil {
		s.fail(w, "create", err)
		return
	}
	s.Hub.Publish(events.Event{Type: "request.updated", OwnerID: req.UserID, Data: req})
	s.Engine.SpawnExtras(context.WithoutCancel(r.Context()), req)
	if s.Settings.Get().Notify.NewRequest {
		s.Notify.Send(context.WithoutCancel(r.Context()), "Nouvelle demande d'extras",
			fmt.Sprintf("**%s** (%s) : %s, demandé par %s", req.Title, req.PlatformName, strings.Join(extras, ", "), req.Username), 3, "gift")
	}
	writeJSON(w, http.StatusCreated, req)
}

func (s *Server) owned(w http.ResponseWriter, r *http.Request) (store.Request, bool) {
	user := userFrom(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return store.Request{}, false
	}
	req, err := s.Store.Get(r.Context(), id)
	// Someone else's request is reported missing, not forbidden.
	if errors.Is(err, store.ErrNotFound) || (err == nil && req.UserID != user.ID && !user.IsAdmin()) {
		writeError(w, http.StatusNotFound, "request not found")
		return store.Request{}, false
	}
	if err != nil {
		s.fail(w, "get", err)
		return store.Request{}, false
	}
	return req, true
}

// cancelRequest works in any open state, downloads included (requester or
// admin). Stopping a running job is the provider's part, added with it.
func (s *Server) cancelRequest(w http.ResponseWriter, r *http.Request) {
	req, ok := s.owned(w, r)
	if !ok {
		return
	}
	if req.State == store.StateAvailable || req.State == store.StateCancelled {
		writeError(w, http.StatusConflict, "request is already "+req.State)
		return
	}
	req, err := s.Engine.Cancel(context.WithoutCancel(r.Context()), req, userFrom(r.Context()).Username)
	if err != nil {
		s.fail(w, "cancel", err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) chooseCandidate(w http.ResponseWriter, r *http.Request) {
	req, ok := s.owned(w, r)
	if !ok {
		return
	}
	var body struct {
		CandidateID string `json:"candidate_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&body); err != nil || body.CandidateID == "" {
		writeError(w, http.StatusBadRequest, "candidate_id is required")
		return
	}
	if req.State != store.StateNeedsChoice && req.State != store.StateWanted && req.State != store.StateFailed {
		writeError(w, http.StatusConflict, "request is "+req.State)
		return
	}
	if err := s.Engine.Choose(context.WithoutCancel(r.Context()), req, body.CandidateID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	req, _ = s.Store.Get(r.Context(), req.ID)
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) retryRequest(w http.ResponseWriter, r *http.Request) {
	req, ok := s.owned(w, r)
	if !ok {
		return
	}
	if req.State != store.StateFailed && req.State != store.StateCancelled {
		writeError(w, http.StatusConflict, "only a failed or cancelled request can be retried")
		return
	}
	req, err := s.Engine.Retry(r.Context(), req)
	if err != nil {
		s.fail(w, "retry", err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) searchAgain(w http.ResponseWriter, r *http.Request) {
	req, ok := s.owned(w, r)
	if !ok {
		return
	}
	if req.State != store.StateWanted && req.State != store.StateNeedsChoice {
		writeError(w, http.StatusConflict, "request is "+req.State)
		return
	}
	req, err := s.Store.Patch(r.Context(), req.ID, map[string]any{"state": store.StateRequested})
	if err != nil {
		s.fail(w, "search", err)
		return
	}
	s.Engine.Wake()
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Settings.Get())
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var body settings.Settings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid settings: "+err.Error())
		return
	}
	if len(body.ResearchMinutes) == 0 || slices.ContainsFunc(body.ResearchMinutes, func(m int) bool { return m < 5 }) {
		writeError(w, http.StatusBadRequest, "re-search delays must be at least 5 minutes")
		return
	}
	saved, err := s.Settings.Save(r.Context(), body)
	if err != nil {
		s.fail(w, "save settings", err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// testTitle runs a live search for a title on a platform and returns every
// result with its verdict and why, rejected ones included. Nothing starts.
func (s *Server) testTitle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title    string `json:"title"`
		Platform string `json:"platform"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&body); err != nil || body.Title == "" || body.Platform == "" {
		writeError(w, http.StatusBadRequest, "title and platform are required")
		return
	}
	ranked, errs, err := s.Engine.Test(r.Context(), body.Title, body.Platform)
	if err != nil {
		s.fail(w, "test", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": ranked, "indexer_errors": errs})
}

func (s *Server) deleteRequest(w http.ResponseWriter, r *http.Request) {
	if !userFrom(r.Context()).IsAdmin() {
		writeError(w, http.StatusForbidden, "admins only")
		return
	}
	req, ok := s.owned(w, r)
	if !ok {
		return
	}
	s.Engine.Discard(context.WithoutCancel(r.Context()), req)
	if err := s.Store.Delete(r.Context(), req.ID); err != nil {
		s.fail(w, "delete", err)
		return
	}
	s.Hub.Publish(events.Event{Type: "request.deleted", OwnerID: req.UserID, Data: map[string]int64{"id": req.ID}})
	w.WriteHeader(http.StatusNoContent)
}

// events streams request changes as server-sent events, with a comment
// every 20 s so proxies keep the connection open.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n: connected\n\n")
	flusher.Flush()

	sub := s.Hub.Subscribe(user.ID, user.IsAdmin())
	defer s.Hub.Unsubscribe(sub)
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		case msg := <-sub.C:
			fmt.Fprintf(w, "data: %s\n\n", msg)
		}
		flusher.Flush()
	}
}

func (s *Server) fail(w http.ResponseWriter, what string, err error) {
	s.Log.Error(what, "err", err)
	writeError(w, http.StatusBadGateway, what+" failed")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
