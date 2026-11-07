package app

import (
	"fmt"
	"net/http"
	"time"
)

func externalListenerKey(appID int64, recipient string) string {
	return fmt.Sprintf("%d:%s", appID, recipient)
}

func (a *App) signalExternal(appID int64, recipient string, messageID int64) {
	key := externalListenerKey(appID, recipient)
	a.mu.Lock()
	defer a.mu.Unlock()
	for ch := range a.externalListeners[key] {
		select {
		case ch <- messageID:
		default:
		}
	}
}

// External recipients receive only hints for their app-scoped stream. They
// must fetch messages via the authenticated /external/messages REST endpoint.
func (a *App) externalEvents(w http.ResponseWriter, r *http.Request) {
	session, ok := a.externalSession(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		failure(w, 500, "unsupported", "SSE unavailable")
		return
	}
	key := externalListenerKey(session.app.id, session.recipient)
	stream := make(chan int64, 16)
	a.mu.Lock()
	if a.externalListeners[key] == nil {
		a.externalListeners[key] = map[chan int64]struct{}{}
	}
	a.externalListeners[key][stream] = struct{}{}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.externalListeners[key], stream)
		if len(a.externalListeners[key]) == 0 {
			delete(a.externalListeners, key)
		}
		a.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		select {
		case id := <-stream:
			fmt.Fprintf(w, "event: messages_available\ndata: {\"last_id\":\"%d\"}\n\n", id)
			flusher.Flush()
		case <-tick.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
