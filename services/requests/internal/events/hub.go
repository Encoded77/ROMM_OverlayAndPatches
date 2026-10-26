// Package events fans request changes out to the open browser tabs (SSE).
package events

import (
	"encoding/json"
	"sync"
)

// Event is one message on the stream. OwnerID scopes it: a user only gets
// events about their own requests, admins get every event.
type Event struct {
	Type    string `json:"type"`
	OwnerID int    `json:"-"`
	Data    any    `json:"data"`
}

type Subscriber struct {
	UserID int
	Admin  bool
	C      chan []byte
}

type Hub struct {
	mu   sync.Mutex
	subs map[*Subscriber]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[*Subscriber]struct{}{}} }

func (h *Hub) Subscribe(userID int, admin bool) *Subscriber {
	s := &Subscriber{UserID: userID, Admin: admin, C: make(chan []byte, 64)}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

func (h *Hub) Unsubscribe(s *Subscriber) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

// Publish never blocks: a tab too slow to keep up misses the event and
// resyncs from the list endpoint when it reconnects.
func (h *Hub) Publish(e Event) {
	payload, err := json.Marshal(e)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		if !s.Admin && s.UserID != e.OwnerID {
			continue
		}
		select {
		case s.C <- payload:
		default:
		}
	}
}
