// Package ttb implements provider.Provider on TorrentTrackerBrowser's API
// (its API.md): SSE search, jobs into a staging storage, malware scan, purge.
package ttb

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/match"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/provider"
)

type Config struct {
	URL     string `json:"url"`     // e.g. http://192.168.1.26:8803
	Storage string `json:"storage"` // staging storage id, e.g. roms-staging
	Engine  string `json:"engine"`  // empty = TTB's default engine
}

type TTB struct {
	cfg    Config
	http   *http.Client
	search *http.Client
	names  map[string]string
}

func New(cfg Config) *TTB {
	return &TTB{
		cfg:    cfg,
		http:   &http.Client{Timeout: 20 * time.Second},
		search: &http.Client{Timeout: 120 * time.Second},
	}
}

func (t *TTB) ID() string { return "ttb" }

type result struct {
	ID         string `json:"id"`
	Indexer    string `json:"indexer"`
	Title      string `json:"title"`
	Size       int64  `json:"size"`
	Seeders    int    `json:"seeders"`
	InfoHash   string `json:"infoHash"`
	HasMagnet  bool   `json:"hasMagnet"`
	HasTorrent bool   `json:"hasTorrent"`
}

func (t *TTB) Search(ctx context.Context, query string) ([]match.Release, []string, error) {
	q := url.Values{"q": {query}, "cat": {"games"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.cfg.URL+"/api/search?"+q.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := t.search.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("ttb search: HTTP %d", resp.StatusCode)
	}

	var releases []match.Release
	var indexerErrors []string
	var event string
	var data strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1<<20), 8<<20)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(line[6:])
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(line[5:]))
		case line == "":
			if data.Len() == 0 {
				continue
			}
			if event == "done" {
				return releases, indexerErrors, nil
			}
			var batch struct {
				Indexer string   `json:"indexer"`
				Results []result `json:"results"`
				Error   *string  `json:"error"`
			}
			if json.Unmarshal([]byte(data.String()), &batch) == nil {
				if batch.Error != nil && *batch.Error != "" {
					indexerErrors = append(indexerErrors, t.indexerName(ctx, batch.Indexer)+": "+reason(*batch.Error))
				}
				for _, r := range batch.Results {
					releases = append(releases, match.Release{
						ID: r.ID, Title: r.Title, Size: r.Size, Seeders: r.Seeders,
						Indexer: t.indexerName(ctx, r.Indexer), InfoHash: strings.ToLower(r.InfoHash),
						Usable: r.HasMagnet || r.HasTorrent,
					})
				}
			}
			event = ""
			data.Reset()
		}
	}
	return releases, indexerErrors, scanner.Err()
}

// reason turns TTB's indexer error ("prowlarr /api/v1/search: HTTP 400:
// {json}") into the readable part: the JSON "message" when there is one.
func reason(s string) string {
	if i := strings.IndexByte(s, '{'); i >= 0 {
		var body struct {
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(s[i:]), &body) == nil && body.Message != "" {
			s = body.Message
		}
	}
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// indexerName resolves "prowlarr:16" to "TR4KER" from TTB's capabilities,
// cached for the life of the provider.
func (t *TTB) indexerName(ctx context.Context, id string) string {
	if t.names == nil {
		var caps struct {
			Sources []struct {
				Indexers []struct{ ID, Name string } `json:"indexers"`
			} `json:"sources"`
		}
		t.names = map[string]string{}
		if t.call(ctx, http.MethodGet, "/api/capabilities", nil, &caps) == nil {
			for _, src := range caps.Sources {
				for _, ix := range src.Indexers {
					t.names[ix.ID] = ix.Name
				}
			}
		}
	}
	if name, ok := t.names[id]; ok {
		return name
	}
	return id
}

func (t *TTB) call(ctx context.Context, method, p string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, t.cfg.URL+p, reader)
	if err != nil {
		return err
	}
	if method != http.MethodGet {
		// TTB's CSRF guard on every mutating call.
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "TorrentTrackerBrowser")
	}
	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("ttb %s %s: HTTP %d %s", method, p, resp.StatusCode, e.Error.Message)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (t *TTB) engine(ctx context.Context) (string, error) {
	if t.cfg.Engine != "" {
		return t.cfg.Engine, nil
	}
	var caps struct {
		Defaults struct{ Engine string } `json:"defaults"`
	}
	if err := t.call(ctx, http.MethodGet, "/api/capabilities", nil, &caps); err != nil {
		return "", err
	}
	return caps.Defaults.Engine, nil
}

func (t *TTB) Start(ctx context.Context, r match.Release, dest provider.Destination) (string, error) {
	engine, err := t.engine(ctx)
	if err != nil {
		return "", err
	}
	var job struct {
		ID string `json:"id"`
	}
	err = t.call(ctx, http.MethodPost, "/api/jobs", map[string]any{
		"resultId": r.ID, "payloadId": nil, "engine": engine, "storage": t.cfg.Storage,
		"subdir": dest.Subdir, "files": nil, "mode": "copy",
	}, &job)
	return job.ID, err
}

type job struct {
	ID       string  `json:"id"`
	InfoHash *string `json:"infoHash"`
	External bool    `json:"external"`
	Storage  *string `json:"storage"`
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
	Speed    *int64  `json:"speed"`
	ETA      *int    `json:"eta"`
	Error    *string `json:"error"`
	Subdir   *string `json:"subdir"`
	Files    []struct {
		Path  string `json:"path"`
		State string `json:"state"`
	} `json:"files"`
	Scan *struct {
		Status string `json:"status"`
	} `json:"scan"`
}

// state maps TTB's job states onto the provider vocabulary.
func state(s string) string {
	switch s {
	case "queued", "adding", "waitingSelection":
		return provider.JobQueued
	case "fetching", "ready", "copying":
		return provider.JobDownloading
	case "scanning":
		return provider.JobScanning
	case "done":
		return provider.JobDone
	case "infected":
		return provider.JobInfected
	case "cancelled":
		return provider.JobCancelled
	default:
		return provider.JobFailed
	}
}

func (t *TTB) Status(ctx context.Context, ids []string) (map[string]provider.JobStatus, error) {
	var list struct {
		Jobs []job `json:"jobs"`
	}
	if err := t.call(ctx, http.MethodGet, "/api/jobs", nil, &list); err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := map[string]provider.JobStatus{}
	for _, j := range list.Jobs {
		if !want[j.ID] {
			continue
		}
		st := provider.JobStatus{State: state(j.State), Progress: j.Progress}
		if j.Speed != nil {
			st.Speed = *j.Speed
		}
		// TorBox reports 8640000 s (100 days) for "unknown" and, once the
		// torrent is complete, a seeding time: neither is a download ETA.
		if j.ETA != nil && *j.ETA > 0 && *j.ETA < 8640000 && j.Progress < 1 {
			st.ETA = *j.ETA
		}
		if j.Error != nil {
			st.Error = *j.Error
		}
		if st.State == provider.JobDone {
			sub := ""
			if j.Subdir != nil {
				sub = *j.Subdir
			}
			for _, f := range j.Files {
				if f.State == "done" {
					st.Files = append(st.Files, path.Join(sub, f.Path))
				}
			}
		}
		out[j.ID] = st
	}
	return out, nil
}

func (t *TTB) Cancel(ctx context.Context, id string, purge bool) error {
	// Cancel first (stops copying, removes the engine item), then delete the
	// job; purge also deletes what it delivered. A job already finished
	// answers 409 to cancel, which is fine.
	_ = t.call(ctx, http.MethodPost, "/api/jobs/"+url.PathEscape(id)+"/cancel", map[string]any{}, nil)
	job := "/api/jobs/" + url.PathEscape(id)
	if purge {
		err := t.call(ctx, http.MethodDelete, job+"?purge=true&deleteFiles=true", nil, nil)
		// TTB only purges cancelled jobs: a finished one is just removed
		// (its delivery in staging is the importer's to clean).
		if err == nil || !strings.Contains(err.Error(), "HTTP 409") {
			return err
		}
	}
	return t.call(ctx, http.MethodDelete, job, nil, nil)
}

func (t *TTB) Find(ctx context.Context, infoHash string, dest provider.Destination) (provider.Existing, bool, error) {
	var list struct {
		Jobs []job `json:"jobs"`
	}
	if err := t.call(ctx, http.MethodGet, "/api/jobs", nil, &list); err != nil {
		return provider.Existing{}, false, err
	}
	for _, j := range list.Jobs {
		sameHash := infoHash != "" && j.InfoHash != nil && strings.EqualFold(*j.InfoHash, infoHash)
		sameDest := j.Storage != nil && *j.Storage == t.cfg.Storage && j.Subdir != nil && *j.Subdir == dest.Subdir
		if !sameHash && !sameDest {
			continue
		}
		st := state(j.State)
		if st == provider.JobFailed || st == provider.JobCancelled || st == provider.JobInfected {
			continue
		}
		ex := provider.Existing{ID: j.ID, State: st, External: j.External}
		if j.Storage != nil {
			ex.Storage = *j.Storage
		}
		if j.Subdir != nil {
			ex.Subdir = *j.Subdir
		}
		return ex, true, nil
	}
	return provider.Existing{}, false, nil
}

func (t *TTB) Adopt(ctx context.Context, ex provider.Existing, dest provider.Destination) (string, bool, error) {
	var j struct {
		ID string `json:"id"`
	}
	switch {
	case ex.External:
		// A torrent added to the engine directly: have TTB copy it into staging.
		err := t.call(ctx, http.MethodPost, "/api/jobs/"+url.PathEscape(ex.ID)+"/send",
			map[string]any{"storage": t.cfg.Storage, "subdir": dest.Subdir, "files": nil, "mode": "copy"}, &j)
		return j.ID, err == nil, err
	case ex.Storage != t.cfg.Storage:
		// Someone's own download elsewhere (shared, manual...): leave it alone.
		return "", false, nil
	case ex.Subdir == dest.Subdir:
		return ex.ID, true, nil
	case ex.State == provider.JobDone:
		// Delivered to staging for another attempt: move it under this one.
		err := t.call(ctx, http.MethodPost, "/api/jobs/"+url.PathEscape(ex.ID)+"/move",
			map[string]any{"storage": t.cfg.Storage, "subdir": dest.Subdir}, &j)
		return j.ID, err == nil, err
	}
	return "", false, nil
}
