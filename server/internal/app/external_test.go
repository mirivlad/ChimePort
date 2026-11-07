package app

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func createTrustedForTest(t *testing.T, h *harness, slug string) string {
	t.Helper()
	status, data := h.req(t, "POST", "/api/v1/admin/external-apps", "admin",
		map[string]any{"slug": slug, "origin": "https://tasks.example.org"}, nil)
	mustStatus(t, status, 201, data)
	return data["token"].(string)
}
func redeemTrustedForTest(t *testing.T, h *harness, appName, recipient string) string {
	t.Helper()
	verifier := strings.Repeat("A", 43)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	status, data := h.req(t, "POST", "/api/v1/external/tickets", appName,
		map[string]string{"recipient": recipient, "device_id": fmt.Sprintf("%032x", len(recipient)+len(appName)), "code_challenge": challenge}, nil)
	mustStatus(t, status, 201, data)
	ticket := data["ticket"].(string)
	status, data = h.req(t, "POST", "/api/v1/external/exchange", "",
		map[string]string{"ticket": ticket, "code_verifier": "incorrect-verifier-that-is-at-least-forty-three-characters"}, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "POST", "/api/v1/external/exchange", "",
		map[string]string{"ticket": ticket, "code_verifier": verifier}, nil)
	mustStatus(t, status, 200, data)
	token := data["access_token"].(string)
	status, data = h.req(t, "POST", "/api/v1/external/exchange", "",
		map[string]string{"ticket": ticket, "code_verifier": verifier}, nil)
	mustStatus(t, status, 401, data)
	return token
}
func TestTrustedApplicationsDoNotCreateUsersOrLeakRecipients(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	h.tokens["tms"] = createTrustedForTest(t, h, "tms")
	h.tokens["crm"] = createTrustedForTest(t, h, "crm")
	status, data := h.req(t, "POST", "/api/v1/external/messages", "tms",
		map[string]any{"recipient": "42", "title": "Assigned", "body": "Task one", "url": "https://tasks.example.org/tasks/1"}, map[string]string{"Idempotency-Key": "evt-one"})
	mustStatus(t, status, 201, data)
	first := data["id"].(string)
	status, data = h.req(t, "POST", "/api/v1/external/messages", "tms",
		map[string]any{"recipient": "42", "title": "Assigned", "body": "Task one", "url": "https://tasks.example.org/tasks/1"}, map[string]string{"Idempotency-Key": "evt-one"})
	mustStatus(t, status, 200, data)
	if data["id"] != first {
		t.Fatal("idempotency returned a new message")
	}
	status, data = h.req(t, "POST", "/api/v1/external/messages", "tms",
		map[string]any{"recipient": "42", "title": "Changed"}, map[string]string{"Idempotency-Key": "evt-one"})
	mustStatus(t, status, 409, data)
	status, data = h.req(t, "POST", "/api/v1/external/messages", "tms",
		map[string]any{"recipient": "43", "title": "Other user's task"}, nil)
	mustStatus(t, status, 201, data)
	otherID := data["id"].(string)
	// An app's link cannot point to arbitrary domains.
	status, data = h.req(t, "POST", "/api/v1/external/messages", "tms",
		map[string]any{"recipient": "42", "title": "Phishing", "url": "https://evil.example.org/"}, nil)
	mustStatus(t, status, 400, data)
	// The bearer for one app cannot access another application's messages.
	status, data = h.req(t, "GET", "/api/v1/external/messages", "tms", nil, nil)
	mustStatus(t, status, 401, data)
	h.tokens["alice-device"] = redeemTrustedForTest(t, h, "tms", "42")
	h.tokens["bob-device"] = redeemTrustedForTest(t, h, "tms", "43")
	h.tokens["crm-device"] = redeemTrustedForTest(t, h, "crm", "42")
	status, data = h.req(t, "GET", "/api/v1/external/messages", "alice-device", nil, nil)
	mustStatus(t, status, 200, data)
	items := data["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != first {
		t.Fatalf("recipient 42 saw foreign messages: %#v", items)
	}
	cursor := data["next_cursor"].(string)
	status, data = h.req(t, "GET", "/api/v1/external/messages?after="+cursor, "bob-device", nil, nil)
	mustStatus(t, status, 400, data)
	status, data = h.req(t, "GET", "/api/v1/external/messages", "crm-device", nil, nil)
	mustStatus(t, status, 200, data)
	if len(data["items"].([]any)) != 0 {
		t.Fatal("another trusted app saw messages")
	}
	status, data = h.req(t, "GET", "/api/v1/external/messages", "bob-device", nil, nil)
	mustStatus(t, status, 200, data)
	if len(data["items"].([]any)) != 1 || data["items"].([]any)[0].(map[string]any)["id"] != otherID {
		t.Fatal("recipient 43 must see only its own messages")
	}
	status, data = h.req(t, "POST", fmt.Sprintf("/api/v1/external/messages/%s/read", otherID), "alice-device", nil, nil)
	mustStatus(t, status, 404, data)
	status, data = h.req(t, "POST", fmt.Sprintf("/api/v1/external/messages/%s/read", first), "alice-device", nil, nil)
	mustStatus(t, status, 200, data)
	status, data = h.req(t, "GET", "/api/v1/external/messages", "alice-device", nil, nil)
	mustStatus(t, status, 200, data)
	if data["items"].([]any)[0].(map[string]any)["read_at"] == nil {
		t.Fatal("read state missing")
	}
	status, data = h.req(t, "GET", "/api/v1/messages", "alice-device", nil, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "POST", "/api/v1/external/messages", "alice-device",
		map[string]any{"recipient": "43", "title": "Unsafe"}, nil)
	mustStatus(t, status, 401, data)
	var users int
	if err := h.store.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&users); err != nil || users != 1 {
		t.Fatalf("external identities created ChimePort user rows: %d %v", users, err)
	}
}
func TestTrustedAppDisableRevokesDeviceAndPublisher(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	h.tokens["tms"] = createTrustedForTest(t, h, "tms")
	h.tokens["device"] = redeemTrustedForTest(t, h, "tms", "42")
	status, data := h.req(t, "GET", "/api/v1/admin/external-apps", "admin", nil, nil)
	mustStatus(t, status, 200, data)
	id := int64(data["items"].([]any)[0].(map[string]any)["id"].(float64))
	status, data = h.req(t, "PATCH", fmt.Sprintf("/api/v1/admin/external-apps/%d", id), "admin", map[string]any{"disabled": true}, nil)
	mustStatus(t, status, 200, data)
	status, data = h.req(t, "GET", "/api/v1/external/messages", "device", nil, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "POST", "/api/v1/external/messages", "tms",
		map[string]any{"recipient": "42", "title": "After disable"}, nil)
	mustStatus(t, status, 401, data)
}
