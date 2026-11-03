package app

import (
	"fmt"
	"testing"
)

func TestUnreadReadToggleIsOwnedAndIdempotent(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	h.addUser(t, "alice")
	h.addUser(t, "bob")
	h.login(t, "alice", "super-long-password")
	h.login(t, "bob", "super-long-password")
	ch := h.channel(t, "alice", "home")
	src := h.source(t, "alice")
	_, publisher := h.sourceToken(t, "alice", src, ch)
	h.tokens["publisher"] = publisher
	status, result := h.req(t, "POST", "/api/v1/messages", "publisher",
		map[string]string{"channel": "home", "title": "Door", "body": "Opened"}, nil)
	mustStatus(t, status, 201, result)
	path := fmt.Sprintf("/api/v1/messages/%s/read", result["id"].(string))
	status, result = h.req(t, "DELETE", path, "bob", nil, nil)
	mustStatus(t, status, 404, result)
	status, result = h.req(t, "DELETE", path, "publisher", nil, nil)
	mustStatus(t, status, 401, result)
	status, result = h.req(t, "DELETE", "/api/v1/messages/xyz/read", "alice", nil, nil)
	mustStatus(t, status, 404, result)
	for _, wantRead := range []bool{true, false, false, true, false} {
		method := "DELETE"
		if wantRead {
			method = "POST"
		}
		status, result = h.req(t, method, path, "alice", nil, nil)
		mustStatus(t, status, 200, result)
		if result["read"] != wantRead {
			t.Fatalf("expected read=%v: %v", wantRead, result)
		}
		status, result = h.req(t, "GET", "/api/v1/messages", "alice", nil, nil)
		mustStatus(t, status, 200, result)
		gotRead := result["items"].([]any)[0].(map[string]any)["read_at"] != nil
		if gotRead != wantRead {
			t.Fatalf("stored read=%v, expected %v", gotRead, wantRead)
		}
	}
}
