// Package realtime provides a small Server-Sent Events hub for in-app
// realtime updates. Publishers never block: a subscriber that cannot keep up
// is disconnected instead of stalling the caller.
package realtime

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// subscriberBuffer is how many events a slow client may fall behind by.
	subscriberBuffer = 64
	// heartbeatInterval keeps proxies and browsers from closing idle streams.
	heartbeatInterval = 25 * time.Second
	// retryMillis tells EventSource how long to wait before reconnecting.
	retryMillis = 3000
)

// Event is one realtime update.
type Event struct {
	ID   string // opaque, monotonically increasing per process when left empty
	Name string // e.g. "entry.created"
	Data []byte // JSON object
}

// Hub fans events out to every current subscriber.
type Hub struct {
	mu     sync.Mutex
	subs   map[*subscriber]struct{}
	nextID uint64
}

type subscriber struct {
	ch     chan Event
	closed bool
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[*subscriber]struct{})}
}

// Publish broadcasts ev to all current subscribers without blocking. Slow
// subscribers are dropped (their buffered channel is closed) rather than
// stalling the caller. If ev.ID is empty a process-monotonic ID is assigned.
func (h *Hub) Publish(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ev.ID == "" {
		h.nextID++
		ev.ID = strconv.FormatUint(h.nextID, 10)
	}

	for sub := range h.subs {
		if sub.closed {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			// Buffer full: drop this subscriber so one stuck client cannot
			// hold up the rest.
			sub.closed = true
			close(sub.ch)
			delete(h.subs, sub)
		}
	}
}

// Subscribers reports how many streams are currently attached.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

func (h *Hub) register() *subscriber {
	sub := &subscriber{ch: make(chan Event, subscriberBuffer)}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

func (h *Hub) unregister(sub *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !sub.closed {
		sub.closed = true
		close(sub.ch)
	}
	delete(h.subs, sub)
}

// ServeHTTP streams events as text/event-stream. It returns when the request
// context is done, which is how the standard library signals a client
// disconnect.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	// Streams are long-lived; a server-wide write deadline (as set by
	// http.Server.WriteTimeout) would cut them off.
	_ = rc.SetWriteDeadline(time.Time{})

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	if _, err := fmt.Fprintf(w, "retry: %d\n\n", retryMillis); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}

	sub := h.register()
	defer h.unregister(sub)

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-sub.ch:
			if !ok {
				// Dropped as a slow subscriber.
				return
			}
			if _, err := w.Write(frame(ev)); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}

		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

// frame renders one SSE event. Multi-line data is split into one data: field
// per line, as the format requires.
func frame(ev Event) []byte {
	var b strings.Builder
	if id := sanitizeField(ev.ID); id != "" {
		b.WriteString("id: ")
		b.WriteString(id)
		b.WriteByte('\n')
	}
	if name := sanitizeField(ev.Name); name != "" {
		b.WriteString("event: ")
		b.WriteString(name)
		b.WriteByte('\n')
	}
	for _, line := range strings.Split(string(ev.Data), "\n") {
		b.WriteString("data: ")
		b.WriteString(strings.TrimSuffix(line, "\r"))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	return []byte(b.String())
}

// sanitizeField replaces characters that would break a single SSE field line
// with spaces, so a hostile event name cannot inject extra fields.
func sanitizeField(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.ReplaceAll(s, "\n", " ")
}
