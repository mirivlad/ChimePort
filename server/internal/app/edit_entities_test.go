package app

import (
	"fmt"
	"strings"
	"testing"
)

func TestEditChannelAndSourcePreservesTokensAndHistory(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	h.addUser(t, "alice")
	h.login(t, "alice", "super-long-password")

	channelID := h.channel(t, "alice", "ha/security")
	sourceID := h.source(t, "alice")
	_, sourceToken := h.sourceToken(t, "alice", sourceID, channelID)
	h.tokens["source"] = sourceToken
	payload := map[string]string{"channel": "ha/security", "title": "Door", "body": "Opened"}
	status, result := h.req(t, "POST", "/api/v1/messages", "source", payload, nil)
	mustStatus(t, status, 201, result)

	status, result = h.req(t, "PATCH", fmt.Sprintf("/api/v1/channels/%d", channelID),
		"alice", map[string]string{"name": "Новый дом", "description": "Оповещения о двери"}, nil)
	mustStatus(t, status, 200, result)
	if result["name"] != "Новый дом" {
		t.Fatalf("channel name did not change: %+v", result)
	}
	status, result = h.req(t, "PATCH", fmt.Sprintf("/api/v1/sources/%d", sourceID),
		"alice", map[string]string{"name": "HA Production", "description": "Датчики"}, nil)
	mustStatus(t, status, 200, result)

	status, result = h.req(t, "GET", "/api/v1/channels", "alice", nil, nil)
	mustStatus(t, status, 200, result)
	items := result["items"].([]any)
	channel := items[0].(map[string]any)
	if channel["slug"] != "ha/security" || channel["name"] != "Новый дом" {
		t.Fatalf("channel slug changed or name not updated: %+v", channel)
	}
	status, result = h.req(t, "POST", "/api/v1/messages", "source", payload, nil)
	mustStatus(t, status, 201, result)
	status, result = h.req(t, "GET", "/api/v1/messages", "alice", nil, nil)
	mustStatus(t, status, 200, result)
	messages := result["items"].([]any)
	if len(messages) != 2 || messages[0].(map[string]any)["source"] != "HA Production" {
		t.Fatalf("messages/token lost by renaming: %+v", messages)
	}
}

func TestCannotEditOtherUsersChannelsOrSources(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	h.addUser(t, "alice")
	h.addUser(t, "bob")
	h.login(t, "alice", "super-long-password")
	h.login(t, "bob", "super-long-password")
	channelID := h.channel(t, "alice", "alice/door")
	sourceID := h.source(t, "alice")

	for _, actor := range []string{"bob", "admin"} {
		for _, path := range []string{
			fmt.Sprintf("/api/v1/channels/%d", channelID),
			fmt.Sprintf("/api/v1/sources/%d", sourceID),
		} {
			status, data := h.req(t, "PATCH", path, actor,
				map[string]string{"name": "stolen", "description": ""}, nil)
			mustStatus(t, status, 404, data)
		}
	}
	status, data := h.req(t, "GET", "/api/v1/channels", "alice", nil, nil)
	mustStatus(t, status, 200, data)
	if data["items"].([]any)[0].(map[string]any)["name"] == "stolen" {
		t.Fatal("another user edited owner's channel")
	}
}

func TestEditRejectsInvalidFieldsAndSlugMutation(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	id := h.channel(t, "admin", "monitoring/alerts")
	src := h.source(t, "admin")
	channelPath := fmt.Sprintf("/api/v1/channels/%d", id)
	sourcePath := fmt.Sprintf("/api/v1/sources/%d", src)
	for _, req := range []struct {
		path string
		body map[string]any
	}{
		{channelPath, map[string]any{"name": "new", "description": "", "slug": "changed"}},
		{channelPath, map[string]any{"name": "   ", "description": ""}},
		{channelPath, map[string]any{"name": strings.Repeat("ж", 101), "description": ""}},
		{sourcePath, map[string]any{"name": "", "description": ""}},
		{sourcePath, map[string]any{"name": "normal", "description": strings.Repeat("x", 1001)}},
	} {
		status, data := h.req(t, "PATCH", req.path, "admin", req.body, nil)
		mustStatus(t, status, 400, data)
	}
	for _, path := range []string{"/api/v1/channels/9999", "/api/v1/sources/9999"} {
		status, data := h.req(t, "PATCH", path, "admin",
			map[string]string{"name": "new", "description": ""}, nil)
		mustStatus(t, status, 404, data)
	}
}
