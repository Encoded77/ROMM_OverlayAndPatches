// Package igdb searches the IGDB catalogue with a Twitch app (the same one
// RomM uses), keeping the app access token until shortly before it expires.
package igdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Game struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Year        int      `json:"year,omitempty"`
	CoverURL    string   `json:"cover_url,omitempty"`
	PlatformIDs []int    `json:"platform_ids"`
	Summary     string   `json:"summary,omitempty"`
	AltNames    []string `json:"alternative_names,omitempty"`
}

type Client struct {
	id, secret string
	http       *http.Client
	// TokenURL and APIURL default to Twitch and IGDB; tests point them at a fake.
	TokenURL string
	APIURL   string

	mu      sync.Mutex
	token   string
	expires time.Time
}

func New(id, secret string) *Client {
	return &Client{id: id, secret: secret, http: &http.Client{Timeout: 15 * time.Second},
		TokenURL: "https://id.twitch.tv/oauth2/token", APIURL: "https://api.igdb.com/v4"}
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.expires) > time.Minute {
		return c.token, nil
	}
	form := url.Values{"client_id": {c.id}, "client_secret": {c.secret}, "grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL+"?"+form.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("twitch token: HTTP %d", resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	c.token, c.expires = body.AccessToken, time.Now().Add(time.Duration(body.ExpiresIn)*time.Second)
	return c.token, nil
}

type rawGame struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	Summary          string `json:"summary"`
	FirstReleaseDate int64  `json:"first_release_date"`
	Cover            *struct {
		ImageID string `json:"image_id"`
	} `json:"cover"`
	Platforms        []int `json:"platforms"`
	AlternativeNames []struct {
		Name    string `json:"name"`
		Comment string `json:"comment"`
	} `json:"alternative_names"`
}

func (r rawGame) toGame() Game {
	g := Game{ID: r.ID, Name: r.Name, Summary: r.Summary, PlatformIDs: r.Platforms}
	if r.FirstReleaseDate > 0 {
		g.Year = time.Unix(r.FirstReleaseDate, 0).UTC().Year()
	}
	if r.Cover != nil && r.Cover.ImageID != "" {
		g.CoverURL = "https://images.igdb.com/igdb/image/upload/t_cover_big/" + r.Cover.ImageID + ".jpg"
	}
	// Most useful for a tracker search first: English titles, then spelling
	// variants (often the unaccented form), then the French title. Names in
	// other languages still help matching, so they follow.
	rank := func(comment string) int {
		c := strings.ToLower(comment)
		switch {
		case strings.Contains(c, "english"):
			return 0
		case strings.Contains(c, "spelling"), strings.Contains(c, "abbreviation"):
			return 1
		case strings.Contains(c, "french"):
			return 2
		}
		return 3
	}
	alts := r.AlternativeNames
	sort.SliceStable(alts, func(i, j int) bool { return rank(alts[i].Comment) < rank(alts[j].Comment) })
	for _, alt := range alts {
		g.AltNames = append(g.AltNames, alt.Name)
	}
	if g.PlatformIDs == nil {
		g.PlatformIDs = []int{}
	}
	return g
}

const fields = "fields name,summary,first_release_date,cover.image_id,platforms,alternative_names.name,alternative_names.comment;"

// Search finds games by name released on any of the given IGDB platforms.
// Main games only: no DLC, mods or bundles (those come later as extras).
func (c *Client) Search(ctx context.Context, term string, platformIDs []int, limit int) ([]Game, error) {
	term = strings.TrimSpace(term)
	if term == "" || len(platformIDs) == 0 {
		return []Game{}, nil
	}
	ids := make([]string, len(platformIDs))
	for i, id := range platformIDs {
		ids[i] = strconv.Itoa(id)
	}
	query := fmt.Sprintf(`search "%s"; %s where platforms = (%s) & version_parent = null & game_type = (0,8,9,10,11); limit %d;`,
		strings.ReplaceAll(term, `"`, ""), fields, strings.Join(ids, ","), limit)
	return c.query(ctx, query)
}

// Related returns IGDB's similar games and the other games of the same
// franchises or collections, for a game page's "missing from the library" row.
func (c *Client) Related(ctx context.Context, id int) ([]Game, error) {
	var refs []struct {
		SimilarGames []int `json:"similar_games"`
		Franchises   []struct {
			Games []int `json:"games"`
		} `json:"franchises"`
		Collections []struct {
			Games []int `json:"games"`
		} `json:"collections"`
	}
	if err := c.raw(ctx, fmt.Sprintf("fields similar_games,franchises.games,collections.games; where id = %d;", id), &refs); err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return []Game{}, nil
	}
	seen := map[int]bool{id: true}
	var ids []string
	add := func(list []int) {
		for _, g := range list {
			if !seen[g] && len(ids) < 60 {
				seen[g] = true
				ids = append(ids, strconv.Itoa(g))
			}
		}
	}
	for _, f := range refs[0].Franchises {
		add(f.Games)
	}
	for _, col := range refs[0].Collections {
		add(col.Games)
	}
	add(refs[0].SimilarGames)
	if len(ids) == 0 {
		return []Game{}, nil
	}
	return c.query(ctx, fmt.Sprintf("%s where id = (%s) & version_parent = null & game_type = (0,8,9,10,11); limit 60;",
		fields, strings.Join(ids, ",")))
}

// Get returns one game by id.
func (c *Client) Get(ctx context.Context, id int) (Game, error) {
	games, err := c.query(ctx, fmt.Sprintf("%s where id = %d;", fields, id))
	if err != nil {
		return Game{}, err
	}
	if len(games) == 0 {
		return Game{}, fmt.Errorf("igdb: game %d not found", id)
	}
	return games[0], nil
}

func (c *Client) query(ctx context.Context, body string) ([]Game, error) {
	var raw []rawGame
	if err := c.raw(ctx, body, &raw); err != nil {
		return nil, err
	}
	games := make([]Game, len(raw))
	for i, r := range raw {
		games[i] = r.toGame()
	}
	return games, nil
}

func (c *Client) raw(ctx context.Context, body string, out any) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.APIURL+"/games", strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Client-ID", c.id)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("igdb: HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
