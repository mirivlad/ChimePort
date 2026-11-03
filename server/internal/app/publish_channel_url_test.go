package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestPublishByFullChannelURL(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	ch := h.channel(t, "admin", "home/security")
	src := h.source(t, "admin")
	_, token := h.sourceToken(t, "admin", src, ch)
	h.tokens["publisher"] = token

	url := "/api/v1/messages?channel=home%2Fsecurity"
	status, data := h.req(t, "POST", url, "publisher", map[string]string{
		"title": "Check", "body": "Published by channel URL", "priority": "high",
	}, nil)
	mustStatus(t, status, 201, data)
	if data["channel"] != "home/security" {
		t.Fatalf("Wrong channel: %v", data["channel"])
	}

	status, data = h.req(t, "POST", url, "publisher", map[string]string{
		"channel": "different", "title": "Check", "body": "Mismatch",
	}, nil)
	mustStatus(t, status, 400, data)
	if data["error"].(map[string]any)["code"] != "channel_mismatch" {
		t.Fatalf("Expected channel_mismatch: %v", data)
	}

	status, data = h.req(t, "POST", "/api/v1/messages?channel=not-allowed", "publisher",
		map[string]string{"title": "Check", "body": "Access denied"}, nil)
	mustStatus(t, status, 403, data)

	status, data = h.req(t, "POST", "/api/v1/messages", "publisher",
		map[string]string{"channel": "home/security", "title": "Legacy", "body": "Still works"}, nil)
	mustStatus(t, status, 201, data)
}

func TestExamplesPageAndScript(t *testing.T) {
	h := setup(t)
	for path, contentType := range map[string]string{
		"/examples":    "text/html",
		"/examples.js": "text/javascript",
	} {
		resp, err := h.server.Client().Get(h.server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), contentType) {
			t.Errorf("GET %s returned %d %q", path, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		resp.Body.Close()
	}
}
