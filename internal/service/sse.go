package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"taskboard-native/internal/model"
)

// pingInterval is how often an idle SSE stream gets a comment keep-alive.
const pingInterval = 15 * time.Second

// subscriber is one SSE connection, optionally scoped to a project.
type subscriber struct {
	projectID string
	events    chan model.Event
	done      chan struct{}
	once      sync.Once
}

func (sub *subscriber) close() {
	sub.once.Do(func() { close(sub.done) })
}

// hub fans store events out to SSE subscribers.
type hub struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

func newHub() *hub { return &hub{subs: map[*subscriber]struct{}{}} }

// publish is the store's OnEvent hook. It never blocks: a subscriber whose
// buffer is full is dropped rather than stalling a write.
func (h *hub) publish(ev model.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		if sub.projectID != "" && sub.projectID != ev.ProjectID {
			continue
		}
		select {
		case sub.events <- ev:
		default:
			delete(h.subs, sub)
			sub.close()
		}
	}
}

func (h *hub) subscribe(projectID string) *subscriber {
	sub := &subscriber{
		projectID: projectID,
		events:    make(chan model.Event, 64),
		done:      make(chan struct{}),
	}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

func (h *hub) unsubscribe(sub *subscriber) {
	h.mu.Lock()
	delete(h.subs, sub)
	h.mu.Unlock()
	sub.close()
}

// closeAll disconnects every subscriber so Close can drain the HTTP server.
func (h *hub) closeAll() {
	h.mu.Lock()
	subs := make([]*subscriber, 0, len(h.subs))
	for sub := range h.subs {
		subs = append(subs, sub)
	}
	h.subs = map[*subscriber]struct{}{}
	h.mu.Unlock()
	for _, sub := range subs {
		sub.close()
	}
}

// handleEvents streams store events as Server-Sent Events, filtered by the
// optional projectId query parameter.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeBadRequest(w, "streaming unsupported")
		return
	}
	projectID := r.URL.Query().Get("projectId")
	sub := s.hub.subscribe(projectID)
	defer s.hub.unsubscribe(sub)

	// Subscribe first, then capture a consistent snapshot. Mutations made
	// after that snapshot stay queued and follow it; older queued events are
	// already represented by the snapshot and can be skipped.
	snapshot, err := s.store.Snapshot(projectID)
	if err != nil {
		http.Error(w, "无法读取看板快照", http.StatusInternalServerError)
		return
	}
	initial, err := json.Marshal(model.Event{
		Seq: snapshot.Seq, Type: model.EventBoardSnapshot,
		Snapshot: &snapshot,
	})
	if err != nil {
		http.Error(w, "无法编码看板快照", http.StatusInternalServerError)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", snapshot.Seq, model.EventBoardSnapshot, initial)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ping := s.ping
	if ping <= 0 {
		ping = pingInterval
	}
	ticker := time.NewTicker(ping)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.done:
			return
		case <-s.closed:
			return
		case ev := <-sub.events:
			if ev.Seq <= snapshot.Seq {
				continue
			}
			raw, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Type, raw); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
