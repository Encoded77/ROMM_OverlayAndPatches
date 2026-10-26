// Package settings holds what an admin can change at run time, stored in
// SQLite and edited from the Demandes settings view.
package settings

import (
	"context"
	"sync"

	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/match"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/provider/ttb"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/store"
)

type Settings struct {
	// AutoDownload lets a confident result start without a human.
	AutoDownload bool `json:"auto_download"`
	// TTB is the download provider.
	TTB ttb.Config `json:"ttb"`
	// Rules per platform folder; missing ones fall back to the built-in defaults.
	Rules map[string]match.Rules `json:"rules"`
	// ResearchMinutes: delays between automatic re-searches of a request
	// nothing confident was found for; the last one repeats.
	ResearchMinutes []int `json:"research_minutes"`
	// Notify chooses which events reach the admin's ntfy topic.
	Notify NotifyEvents `json:"notify"`
}

type NotifyEvents struct {
	NewRequest  bool `json:"new_request"`
	NeedsChoice bool `json:"needs_choice"`
	Failed      bool `json:"failed"`
	Available   bool `json:"available"`
}

func Defaults(ttbURL, ttbStorage string) Settings {
	return Settings{
		AutoDownload:    true,
		TTB:             ttb.Config{URL: ttbURL, Storage: ttbStorage},
		Rules:           match.DefaultRules(),
		ResearchMinutes: []int{15, 60, 360, 1440, 10080},
		Notify:          NotifyEvents{NewRequest: true, NeedsChoice: true, Failed: true, Available: true},
	}
}

// Holder caches the settings; every reader gets a consistent copy.
type Holder struct {
	mu       sync.RWMutex
	current  Settings
	store    *store.Store
	defaults Settings
}

const key = "settings"

func Load(ctx context.Context, s *store.Store, defaults Settings) (*Holder, error) {
	h := &Holder{store: s, defaults: defaults, current: defaults}
	var saved Settings
	found, err := s.Setting(ctx, key, &saved)
	if err != nil {
		return nil, err
	}
	if found {
		h.current = merge(defaults, saved)
	}
	return h, nil
}

// merge keeps defaults for anything a saved copy lacks (a platform added to
// the library later, a field added in a newer version).
func merge(defaults, saved Settings) Settings {
	out := saved
	if out.TTB.URL == "" {
		out.TTB.URL = defaults.TTB.URL
	}
	if out.TTB.Storage == "" {
		out.TTB.Storage = defaults.TTB.Storage
	}
	if len(out.ResearchMinutes) == 0 {
		out.ResearchMinutes = defaults.ResearchMinutes
	}
	if out.Rules == nil {
		out.Rules = map[string]match.Rules{}
	}
	for slug, r := range defaults.Rules {
		if _, ok := out.Rules[slug]; !ok {
			out.Rules[slug] = r
		}
	}
	return out
}

func (h *Holder) Get() Settings {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.current
}

func (h *Holder) Save(ctx context.Context, s Settings) (Settings, error) {
	s = merge(h.defaults, s)
	if err := h.store.SetSetting(ctx, key, s); err != nil {
		return Settings{}, err
	}
	h.mu.Lock()
	h.current = s
	h.mu.Unlock()
	return s, nil
}
