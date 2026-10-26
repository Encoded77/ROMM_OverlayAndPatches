// Package provider is the seam between the request engine and whatever finds
// and downloads releases. TTB implements it today; Prowlarr can later.
package provider

import (
	"context"

	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/match"
)

// Job states, as the engine sees them.
const (
	JobQueued      = "queued"
	JobDownloading = "downloading"
	JobScanning    = "scanning" // malware scan after the download
	JobDone        = "done"
	JobFailed      = "failed"
	JobInfected    = "infected"
	JobCancelled   = "cancelled"
)

type Destination struct {
	// Subdir under the provider's staging storage, one per request.
	Subdir string
}

type JobStatus struct {
	State    string  `json:"state"`
	Progress float64 `json:"progress"` // 0..1
	Speed    int64   `json:"speed"`    // bytes/s
	ETA      int     `json:"eta"`      // seconds, 0 = unknown
	Error    string  `json:"error,omitempty"`
	// Files delivered, relative to the staging storage, once done.
	Files []string `json:"files,omitempty"`
}

// Existing is a job the provider already has for a torrent.
type Existing struct {
	ID       string
	State    string // provider job state (JobDone, JobDownloading...)
	Storage  string
	Subdir   string
	External bool // not started by the provider (added in the engine directly)
}

type Provider interface {
	ID() string
	// Search returns every result for the query (all indexers, errors of a
	// single indexer are tolerated and reported in the second value).
	Search(ctx context.Context, query string) ([]match.Release, []string, error)
	// Start downloads a result into the staging storage.
	Start(ctx context.Context, release match.Release, dest Destination) (jobID string, err error)
	// Status of every job this provider started that is still known.
	Status(ctx context.Context, jobIDs []string) (map[string]JobStatus, error)
	// Cancel stops a job; purge also deletes what it delivered.
	Cancel(ctx context.Context, jobID string, purge bool) error
	// Find looks for a job already handling this torrent: same info hash, or
	// a live job already delivering into dest (an earlier attempt).
	Find(ctx context.Context, infoHash string, dest Destination) (Existing, bool, error)
	// Adopt makes an existing job deliver into dest instead of starting a
	// second download; ok is false when it cannot be reused.
	Adopt(ctx context.Context, ex Existing, dest Destination) (jobID string, ok bool, err error)
}
