// Package store keeps requests in SQLite (pure Go driver, WAL).
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// States of a request. Phase 1 only creates `requested` and `cancelled`; the
// provider and importer phases move requests through the rest.
const (
	StateRequested   = "requested"
	StateSearching   = "searching"
	StateNeedsChoice = "needs_choice"
	StateWanted      = "wanted"
	StateDownloading = "downloading"
	StateScanning    = "scanning"
	StateImporting   = "importing"
	StateAvailable   = "available"
	StateFailed      = "failed"
	StateCancelled   = "cancelled"
)

var ErrNotFound = errors.New("request not found")

type Request struct {
	ID           int64     `json:"id"`
	UserID       int       `json:"user_id"`
	Username     string    `json:"username"`
	IGDBID       int       `json:"igdb_id"`
	Title        string    `json:"title"`
	Year         int       `json:"year,omitempty"`
	CoverURL     string    `json:"cover_url,omitempty"`
	PlatformSlug string    `json:"platform_slug"`
	PlatformName string    `json:"platform_name"`
	Extras       []string  `json:"extras"`
	State        string    `json:"state"`
	Detail       string    `json:"detail,omitempty"`
	Progress     *float64  `json:"progress,omitempty"`
	RomID        *int      `json:"rom_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	Provider     string          `json:"provider,omitempty"`
	JobID        string          `json:"-"`
	Candidates   json.RawMessage `json:"candidates"`
	Chosen       json.RawMessage `json:"chosen,omitempty"`
	Searches     int             `json:"searches"`
	NextSearchAt string          `json:"next_search_at,omitempty"`
	SearchedAt   string          `json:"searched_at,omitempty"`
	Placed       string          `json:"placed,omitempty"`
	ParentID     *int64          `json:"parent_id,omitempty"`
	Extra        string          `json:"extra,omitempty"`
	AltNames     []string        `json:"-"`
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// One writer at a time is SQLite's model; one connection avoids SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var done int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE name = ?`, name).Scan(&done); err != nil {
			return err
		}
		if done > 0 {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		// The driver runs one statement per Exec; migrations hold no ';'
		// inside literals, so splitting on it is safe.
		for _, stmt := range strings.Split(string(body), ";") {
			if strings.TrimSpace(stripComments(stmt)) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations VALUES (?, ?)`, name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func stripComments(stmt string) string {
	var out []string
	for _, line := range strings.Split(stmt, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

const columns = `id, user_id, username, igdb_id, title, year, cover_url, platform_slug, platform_name,
	extras, state, detail, progress, rom_id, created_at, updated_at,
	provider, job_id, candidates, chosen, searches, next_search_at, searched_at, placed, parent_id, extra, alt_names`

func scan(row interface{ Scan(...any) error }) (Request, error) {
	var r Request
	var extras, created, updated string
	var progress sql.NullFloat64
	var romID, parent sql.NullInt64
	var candidates, chosen, alt string
	if err := row.Scan(&r.ID, &r.UserID, &r.Username, &r.IGDBID, &r.Title, &r.Year, &r.CoverURL,
		&r.PlatformSlug, &r.PlatformName, &extras, &r.State, &r.Detail, &progress, &romID, &created, &updated,
		&r.Provider, &r.JobID, &candidates, &chosen, &r.Searches, &r.NextSearchAt, &r.SearchedAt, &r.Placed,
		&parent, &r.Extra, &alt); err != nil {
		return r, err
	}
	r.Candidates = json.RawMessage(candidates)
	if chosen != "" {
		r.Chosen = json.RawMessage(chosen)
	}
	if parent.Valid {
		r.ParentID = &parent.Int64
	}
	_ = json.Unmarshal([]byte(alt), &r.AltNames)
	_ = json.Unmarshal([]byte(extras), &r.Extras)
	if r.Extras == nil {
		r.Extras = []string{}
	}
	if progress.Valid {
		r.Progress = &progress.Float64
	}
	if romID.Valid {
		id := int(romID.Int64)
		r.RomID = &id
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return r, nil
}

// Create stores a new request in state `requested`.
func (s *Store) Create(ctx context.Context, r Request) (Request, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	extras, _ := json.Marshal(r.Extras)
	alt, _ := json.Marshal(r.AltNames)
	res, err := s.db.ExecContext(ctx, `INSERT INTO requests
		(user_id, username, igdb_id, title, year, cover_url, platform_slug, platform_name, extras, state, detail,
		 created_at, updated_at, parent_id, extra, alt_names)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?)`,
		r.UserID, r.Username, r.IGDBID, r.Title, r.Year, r.CoverURL, r.PlatformSlug, r.PlatformName, string(extras), StateRequested,
		now, now, r.ParentID, r.Extra, string(alt))
	if err != nil {
		return Request{}, err
	}
	id, _ := res.LastInsertId()
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id int64) (Request, error) {
	r, err := scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM requests WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// List returns requests newest first; userID 0 means every user's.
// history=true lists finished requests older than 30 days, false the rest.
func (s *Store) List(ctx context.Context, userID int, history bool) ([]Request, error) {
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
	where := `(state NOT IN ('available','failed','cancelled') OR updated_at >= ?)`
	if history {
		where = `(state IN ('available','failed','cancelled') AND updated_at < ?)`
	}
	args := []any{cutoff}
	if userID != 0 {
		where += ` AND user_id = ?`
		args = append(args, userID)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM requests WHERE `+where+` ORDER BY created_at DESC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Request{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// OpenFor returns the user's open request for this game and platform, if any.
func (s *Store) OpenFor(ctx context.Context, userID, igdbID int, platform string) (*Request, error) {
	r, err := scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM requests
		WHERE user_id = ? AND igdb_id = ? AND platform_slug = ? AND state NOT IN ('available','failed','cancelled')
		LIMIT 1`, userID, igdbID, platform))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// SetState moves a request to a state with an explanation.
func (s *Store) SetState(ctx context.Context, id int64, state, detail string) (Request, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE requests SET state = ?, detail = ?, updated_at = ? WHERE id = ?`,
		state, detail, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return Request{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Request{}, ErrNotFound
	}
	return s.Get(ctx, id)
}

// Patch updates the given columns (and updated_at) of one request. Column
// names come from this package only, never from user input.
func (s *Store) Patch(ctx context.Context, id int64, fields map[string]any) (Request, error) {
	sets := []string{"updated_at = ?"}
	args := []any{time.Now().UTC().Format(time.RFC3339Nano)}
	for col, val := range fields {
		sets = append(sets, col+" = ?")
		switch v := val.(type) {
		case json.RawMessage:
			args = append(args, string(v))
		default:
			args = append(args, v)
		}
	}
	args = append(args, id)
	res, err := s.db.ExecContext(ctx, `UPDATE requests SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return Request{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Request{}, ErrNotFound
	}
	return s.Get(ctx, id)
}

// ByStates lists requests in any of the given states, oldest first.
func (s *Store) ByStates(ctx context.Context, states ...string) ([]Request, error) {
	marks := strings.TrimSuffix(strings.Repeat("?,", len(states)), ",")
	args := make([]any, len(states))
	for i, st := range states {
		args[i] = st
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM requests WHERE state IN (`+marks+`) ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Request
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// Setting reads one JSON setting into out; missing is not an error.
func (s *Store) Setting(ctx context.Context, key string, out any) (bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(value), out)
}

func (s *Store) SetSetting(ctx context.Context, key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(b))
	return err
}

// Delete removes a request for good (admins, history cleanup).
func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM requests WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
