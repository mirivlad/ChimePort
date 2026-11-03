package app

import (
	"fmt"
	"testing"
)

func TestTokenPermissionsUpdatedWithoutRotation(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	h.addUser(t, "alice")
	h.addUser(t, "bob")
	h.login(t, "alice", "super-long-password")
	h.login(t, "bob", "super-long-password")
	first := h.channel(t, "alice", "home")
	second := h.channel(t, "alice", "monitoring")
	foreign := h.channel(t, "bob", "private")
	src := h.source(t, "alice")
	tokenID, secret := h.sourceToken(t, "alice", src, first)
	h.tokens["publisher"] = secret
	path := fmt.Sprintf("/api/v1/sources/%d/tokens/%d", src, tokenID)
	send := func(channel string) (int, map[string]any) {
		return h.req(t, "POST", "/api/v1/messages", "publisher", map[string]any{"channel": channel, "title": "Check", "body": "ok"}, nil)
	}
	status, data := send("home")
	mustStatus(t, status, 201, data)
	status, data = send("monitoring")
	mustStatus(t, status, 403, data)

	status, data = h.req(t, "PATCH", path, "alice", map[string]any{"channel_ids": []int64{second}}, nil)
	mustStatus(t, status, 200, data)
	status, data = send("home")
	mustStatus(t, status, 403, data)
	status, data = send("monitoring")
	mustStatus(t, status, 201, data)
	status, data = h.req(t, "GET", path+"/secret", "alice", nil, nil)
	mustStatus(t, status, 200, data)
	if data["token"] != secret {
		t.Fatal("Permissions update changed the token")
	}

	// Each invalid attempt leaves the previous permission set intact.
	for _, tc := range []struct {
		name string
		ids  []int64
		code int
	}{
		{"empty", []int64{}, 400},
		{"foreign", []int64{first, foreign}, 400},
		{"duplicate", []int64{first, first}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, result := h.req(t, "PATCH", path, "alice", map[string]any{"channel_ids": tc.ids}, nil)
			mustStatus(t, code, tc.code, result)
			code, result = send("monitoring")
			mustStatus(t, code, 201, result)
			code, result = send("home")
			mustStatus(t, code, 403, result)
		})
	}
	status, data = h.req(t, "PATCH", path, "bob", map[string]any{"channel_ids": []int64{foreign}}, nil)
	mustStatus(t, status, 404, data)
	status, data = h.req(t, "PATCH", path, "publisher", map[string]any{"channel_ids": []int64{first}}, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "GET", fmt.Sprintf("/api/v1/sources/%d/tokens", src), "alice", nil, nil)
	mustStatus(t, status, 200, data)
	channels := data["items"].([]any)[0].(map[string]any)["allowed_channels"].([]any)
	if len(channels) != 1 || channels[0] != "monitoring" {
		t.Fatalf("wrong updated metadata: %v", channels)
	}
	status, data = h.req(t, "DELETE", path, "alice", nil, nil)
	mustStatus(t, status, 200, data)
	status, data = h.req(t, "PATCH", path, "alice", map[string]any{"channel_ids": []int64{first}}, nil)
	mustStatus(t, status, 404, data)
}
