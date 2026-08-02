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

	// Cursor precedence: Last-Event-ID wins over ?after. The header is the
	// browser's own resume signal, sent automatically on reconnect, while
	// `after` is where the client asked to start when it first connected.
	// Letting `after` win would make every reconnect replay from that original
	// point — a client that opened at `after=0` would re-read the whole log on
	// each drop, which is the opposite of resuming.
	var cursor uint64
	if v := r.URL.Query().Get("after"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			cursor = n
		}
	}
	if v := r.Header.Get("Last-Event-ID"); v != "" {
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

	// Replay backlog, then tail from where the replay actually ended. Starting
	// the tail at `cursor` instead would re-send the whole backlog on the first
	// tick — every event delivered twice, which breaks the "no duplicate"
	// guarantee this stream is supposed to provide.
	last, err := s.sendEvents(w, flusher, id, cursor)
	if err != nil {
		return
	}
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

// sendEvents writes all events with seq > cursor and returns the last seq sent
// (or cursor unchanged when there was nothing to send), so the caller can
// continue tailing from exactly where the replay stopped.
func (s *Server) sendEvents(w http.ResponseWriter, fl http.Flusher, id string, cursor uint64) (uint64, error) {
	evs, err := s.store.ReadEvents(id, cursor, 0)
	if err != nil {
		return cursor, err
	}
	last := cursor
	for _, e := range evs {
		s.writeSSE(w, fl, e)
		last = e.Seq
	}
	return last, nil
}

func (s *Server) writeSSE(w http.ResponseWriter, fl http.Flusher, e *model.Event) {
	data, _ := json.Marshal(e)
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Kind, data)
	fl.Flush()
}
