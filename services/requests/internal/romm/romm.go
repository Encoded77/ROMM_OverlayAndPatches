// Package romm talks to RomM's REST API: who a browser session belongs to
// (with the user's own cookie) and what the library holds (with a token).
package romm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrUnauthenticated means the cookie does not belong to a logged-in user.
var ErrUnauthenticated = errors.New("not logged in to RomM")

type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (u User) IsAdmin() bool { return u.Role == "admin" }

type Platform struct {
	ID     int    `json:"id"`
	Slug   string `json:"slug"`
	FSSlug string `json:"fs_slug"`
	Name   string `json:"name"`
	IGDBID *int   `json:"igdb_id"`
}

type Client struct {
	base  string
	token string
	http  *http.Client
	ttl   time.Duration

	mu       sync.Mutex
	sessions map[[32]byte]cachedUser
	plats    []Platform
	platsAt  time.Time
}

type cachedUser struct {
	user User
	at   time.Time
}

func New(base, token string, ttl time.Duration) *Client {
	return &Client{
		base: base, token: token, ttl: ttl,
		http:     &http.Client{Timeout: 15 * time.Second},
		sessions: map[[32]byte]cachedUser{},
	}
}

// UserFromCookie asks RomM who the browser's cookie header belongs to. The
// answer is cached briefly so a page full of calls costs RomM one request.
func (c *Client) UserFromCookie(ctx context.Context, cookie string) (User, error) {
	if cookie == "" {
		return User{}, ErrUnauthenticated
	}
	key := sha256.Sum256([]byte(cookie))
	c.mu.Lock()
	if hit, ok := c.sessions[key]; ok && time.Since(hit.at) < c.ttl {
		c.mu.Unlock()
		return hit.user, nil
	}
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/users/me", nil)
	if err != nil {
		return User{}, err
	}
	req.Header.Set("Cookie", cookie)
	var user User
	if err := c.do(req, &user); err != nil {
		var status statusError
		if errors.As(err, &status) && (status == http.StatusUnauthorized || status == http.StatusForbidden) {
			return User{}, ErrUnauthenticated
		}
		return User{}, err
	}

	c.mu.Lock()
	c.sessions[key] = cachedUser{user: user, at: time.Now()}
	// Keep the cache bounded: sessions are few, a sweep on growth is enough.
	if len(c.sessions) > 1000 {
		for k, v := range c.sessions {
			if time.Since(v.at) >= c.ttl {
				delete(c.sessions, k)
			}
		}
	}
	c.mu.Unlock()
	return user, nil
}

// Platforms is the library's platform list, cached for ten minutes.
func (c *Client) Platforms(ctx context.Context) ([]Platform, error) {
	c.mu.Lock()
	if c.plats != nil && time.Since(c.platsAt) < 10*time.Minute {
		defer c.mu.Unlock()
		return c.plats, nil
	}
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/platforms", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	var plats []Platform
	if err := c.do(req, &plats); err != nil {
		// A slow or restarting RomM must not break searches: the platform
		// list barely changes, the last good copy will do.
		c.mu.Lock()
		stale := c.plats
		c.mu.Unlock()
		if stale != nil {
			return stale, nil
		}
		return nil, err
	}
	c.mu.Lock()
	c.plats, c.platsAt = plats, time.Now()
	c.mu.Unlock()
	return plats, nil
}

type statusError int

func (s statusError) Error() string { return fmt.Sprintf("RomM answered HTTP %d", int(s)) }

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError(resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Rom is the part of a RomM rom record the importer needs.
type Rom struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	FSName   string `json:"fs_name"`
	FSPath   string `json:"fs_path"`
	Platform string `json:"platform_fs_slug"`
	IGDBID   *int   `json:"igdb_id"`
	Missing  bool   `json:"missing_from_fs"`
}

// FindRom looks a placed file or folder up in the library by its name on
// disk, within one platform. Zero ID means not (yet) in the library.
func (c *Client) FindRom(ctx context.Context, platformID int, fsName string) (Rom, error) {
	stem := strings.TrimSuffix(fsName, path.Ext(fsName))
	q := url.Values{
		"platform_ids":     {strconv.Itoa(platformID)},
		"search_term":      {stem},
		"group_by_meta_id": {"false"},
		"limit":            {"50"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/roms?"+q.Encode(), nil)
	if err != nil {
		return Rom{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	var page struct {
		Items []Rom `json:"items"`
	}
	if err := c.do(req, &page); err != nil {
		return Rom{}, err
	}
	for _, r := range page.Items {
		if r.FSName == fsName && !r.Missing {
			return r, nil
		}
	}
	return Rom{}, nil
}

// HasGame reports whether the library already holds this IGDB game on a platform.
func (c *Client) HasGame(ctx context.Context, platformID, igdbID int, title string) (Rom, error) {
	q := url.Values{
		"platform_ids":     {strconv.Itoa(platformID)},
		"search_term":      {title},
		"group_by_meta_id": {"false"},
		"limit":            {"50"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/roms?"+q.Encode(), nil)
	if err != nil {
		return Rom{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	var page struct {
		Items []Rom `json:"items"`
	}
	if err := c.do(req, &page); err != nil {
		return Rom{}, err
	}
	for _, r := range page.Items {
		if r.IGDBID != nil && *r.IGDBID == igdbID && !r.Missing {
			return r, nil
		}
	}
	return Rom{}, nil
}

// MatchIGDB tells RomM which IGDB game a rom is; RomM then fetches its
// metadata itself. Needs the roms.write scope.
func (c *Client) MatchIGDB(ctx context.Context, romID, igdbID int) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("igdb_id", strconv.Itoa(igdbID))
	w.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("%s/api/roms/%d", c.base, romID), &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError(resp.StatusCode)
	}
	return nil
}

// GetRom reads one rom (where it lives on disk).
func (c *Client) GetRom(ctx context.Context, id int) (Rom, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/roms/%d", c.base, id), nil)
	if err != nil {
		return Rom{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	var rom Rom
	err = c.do(req, &rom)
	return rom, err
}
