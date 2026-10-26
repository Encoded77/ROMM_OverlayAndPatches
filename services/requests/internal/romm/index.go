package romm

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/match"
)

// Index is the library as a set of (IGDB id, platform) pairs, refreshed in
// the background, so search results can say what is already owned.
type Index struct {
	c   *Client
	log *slog.Logger

	mu    sync.RWMutex
	games map[int]map[string]int // igdb id -> fs_slug -> rom id
	names map[string][]named     // fs_slug -> every rom, by title key
	ready bool
}

// Named is a library ROM a title resembles.
type Named struct {
	RomID int    `json:"rom_id"`
	Name  string `json:"name"`
}

type named struct {
	Named
	key string
}

func NewIndex(c *Client, log *slog.Logger) *Index {
	return &Index{c: c, log: log, games: map[int]map[string]int{}}
}

// Run refreshes every interval until ctx ends. A failed refresh (RomM
// restarting, typically) is retried after a minute, not a full interval:
// until one succeeds, search results cannot say what is already owned.
func (x *Index) Run(ctx context.Context, every time.Duration) {
	for {
		wait := every
		if err := x.refresh(ctx); err != nil {
			x.log.Warn("library index", "err", err)
			wait = time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (x *Index) refresh(ctx context.Context) error {
	games := map[int]map[string]int{}
	names := map[string][]named{}
	for offset := 0; ; offset += 500 {
		q := url.Values{"limit": {"500"}, "offset": {strconv.Itoa(offset)}, "order_by": {"id"},
			"order_dir": {"asc"}, "group_by_meta_id": {"false"}}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, x.c.base+"/api/roms?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+x.c.token)
		var page struct {
			Items []struct {
				ID       int    `json:"id"`
				IGDBID   *int   `json:"igdb_id"`
				Name     string `json:"name"`
				FSName   string `json:"fs_name_no_ext"`
				Platform string `json:"platform_fs_slug"`
				Missing  bool   `json:"missing_from_fs"`
			} `json:"items"`
		}
		if err := x.c.do(req, &page); err != nil {
			return err
		}
		for _, r := range page.Items {
			if r.Missing {
				continue
			}
			name := r.Name
			if name == "" {
				name = r.FSName
			}
			names[r.Platform] = append(names[r.Platform], named{Named{r.ID, name}, match.Key(name)})
			if r.IGDBID == nil {
				continue
			}
			if games[*r.IGDBID] == nil {
				games[*r.IGDBID] = map[string]int{}
			}
			games[*r.IGDBID][r.Platform] = r.ID
		}
		if len(page.Items) < 500 {
			break
		}
	}
	x.mu.Lock()
	x.games, x.names, x.ready = games, names, true
	x.mu.Unlock()
	x.log.Info("library index", "games", len(games))
	return nil
}

// Ready reports whether a first refresh has completed.
func (x *Index) Ready() bool {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.ready
}

// Owned returns the platforms (fs_slug -> rom id) a game is in the library on.
func (x *Index) Owned(igdbID int) map[string]int {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.games[igdbID]
}

// Resembling finds a ROM on the platform whose title is this one or starts
// with it, for titles of two words or more ("Superstar Saga" in "Superstar Saga + Bowser's Minions"): IGDB
// sometimes files one game under two entries (remake, bundle), so the id
// check alone misses it.
func (x *Index) Resembling(platform, title string) (Named, bool) {
	k := match.Key(title)
	if k == "" {
		return Named{}, false
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	for _, n := range x.names[platform] {
		// A one-word title only matches exactly: "Mario" is in too many names.
		if n.key == k || strings.Contains(k, " ") && strings.HasPrefix(n.key, k+" ") {
			return n.Named, true
		}
	}
	return Named{}, false
}

// Add records a game the importer just linked, before the next refresh.
func (x *Index) Add(igdbID int, platform string, romID int) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.games[igdbID] == nil {
		x.games[igdbID] = map[string]int{}
	}
	x.games[igdbID][platform] = romID
}
