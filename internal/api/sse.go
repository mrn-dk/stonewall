package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/mrn-dk/stonewall/internal/model"
)

// streamEvents streams an agent's event log as Server-Sent Events. On connect
// it replays forward from the client's cursor (Last-Event-ID, or the `after`
// query param) to the current head out of the durable log, then continues
// tailing as new events are appended. The stream is the durable log, so the
// live view and the audit record are the same object, and reconnect resumes
// exactly ("stream from event N", spec §4.5, §5.1).
//
// Heartbeat comments keep intermediaries from reaping idle connections. The
// stream terminates cleanly when the agent is terminal and the tail is caught
// up.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.GetAgent(id); err != nil {
		notFound(w, "agent not found")
		return
	}

	var cursor uint64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			cursor = n
		}
	}
	if v := r.URL.Query().Get("after"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			cursor = n
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		internalErr(w, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()
	tailInterval := 250 * time.Millisecond
	heartbeat := time.NewTicker(5 * time.Second)
	defer heartbeat.Stop()

	// Replay backlog.
	if err := s.sendEvents(w, flusher, id, cursor); err != nil {
		return
	}

	// Tail live.
	last := cursor
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-time.After(tailInterval):
			evs, err := s.store.ReadEvents(id, last, 0)
			if err != nil {
				return
			}
			for _, e := range evs {
				s.writeSSE(w, flusher, e)
				last = e.Seq
			}
			// Stop when the agent is terminal and we've drained the log.
			if a, err := s.store.GetAgent(id); err == nil && a.State.Terminal() {
				// One final drain already done above; confirm no new events.
				evs2, _ := s.store.ReadEvents(id, last, 0)
				if len(evs2) == 0 {
					fmt.Fprintf(w, "event: done\ndata: {\"state\":%q}\n\n", a.State)
					flusher.Flush()
					return
				}
				for _, e := range evs2 {
					s.writeSSE(w, flusher, e)
					last = e.Seq
				}
			}
		}
	}
}

// sendEvents writes all events with seq > cursor and returns the last seq sent.
func (s *Server) sendEvents(w http.ResponseWriter, fl http.Flusher, id string, cursor uint64) error {
	evs, err := s.store.ReadEvents(id, cursor, 0)
	if err != nil {
		return err
	}
	for _, e := range evs {
		s.writeSSE(w, fl, e)
	}
	return nil
}

func (s *Server) writeSSE(w http.ResponseWriter, fl http.Flusher, e *model.Event) {
	data, _ := json.Marshal(e)
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Kind, data)
	fl.Flush()
}
