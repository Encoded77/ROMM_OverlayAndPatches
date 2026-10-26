// Package notify publishes admin notifications to an ntfy topic.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Ntfy struct {
	base, topic, token string
	click              string
	http               *http.Client
	log                *slog.Logger
}

// New takes the topic URL (https://ntfy.example/topic) and an access token;
// an empty URL gives a notifier that does nothing.
func New(topicURL, token, click string, log *slog.Logger) *Ntfy {
	n := &Ntfy{token: token, click: click, http: &http.Client{Timeout: 10 * time.Second}, log: log}
	if u, err := url.Parse(topicURL); err == nil && u.Host != "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		n.topic = parts[len(parts)-1]
		u.Path = "/" + strings.Join(parts[:len(parts)-1], "/")
		n.base = strings.TrimRight(u.String(), "/")
	}
	return n
}

// Send never fails the caller: notifications are best effort.
func (n *Ntfy) Send(ctx context.Context, title, message string, priority int, tags ...string) {
	if n.topic == "" {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"topic": n.topic, "title": title, "message": message, "priority": priority,
		"tags": tags, "markdown": true, "click": n.click,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.base, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if n.token != "" {
		req.Header.Set("Authorization", "Bearer "+n.token)
	}
	resp, err := n.http.Do(req)
	if err != nil {
		n.log.Warn("ntfy", "err", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		n.log.Warn("ntfy", "err", fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
}
