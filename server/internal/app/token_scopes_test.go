package app

import (
	"fmt"
	"testing"
)

func TestTokenListIncludesAllowedChannelsWithoutLeakingSecrets(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	h.addUser(t, "alice")
	h.addUser(t, "bob")
	h.login(t, "alice", "super-long-password")
	h.login(t, "bob", "super-long-password")

	first := h.channel(t, "alice", "home/security")
	second := h.channel(t, "alice", "server/backup")
	_ = h.channel(t, "bob", "private")
	sourceID := h.source(t, "alice")
	status, data := h.req(t, "POST", fmt.Sprintf("/api/v1/sources/%d/tokens", sourceID), "alice", map[string]any{
		"label":       "test token",
		"channel_ids": []int64{first, second},
	}, nil)
	mustStatus(t, status, 201, data)

	path := fmt.Sprintf("/api/v1/sources/%d/tokens", sourceID)
	status, data = h.req(t, "GET", path, "alice", nil, nil)
	mustStatus(t, status, 200, data)
	records := data["items"].([]any)
	if len(records) != 1 {
		t.Fatalf("expected one token, got %d", len(records))
	}
	record := records[0].(map[string]any)
	if _, leaked := record["token"]; leaked {
		t.Fatal("token secret leaked in listing")
	}
	scopes, ok := record["allowed_channels"].([]any)
	if !ok || len(scopes) != 2 {
		t.Fatalf("missing allowed channel slugs: %#v", record["allowed_channels"])
	}
	found := map[string]bool{}
	for _, item := range scopes {
		found[item.(string)] = true
	}
	if !found["home/security"] || !found["server/backup"] || found["private"] {
		t.Fatalf("wrong scopes: %#v", found)
	}

	status, data = h.req(t, "GET", path, "bob", nil, nil)
	mustStatus(t, status, 404, data)
}
