package app

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestChangeOwnPasswordRevokesEverySession(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	h.addUser(t, "alice")
	h.login(t, "alice", "super-long-password")
	firstToken := h.tokens["alice"]
	h.login(t, "alice", "super-long-password")
	secondToken := h.tokens["alice"]

	status, data := h.req(t, "POST", "/api/v1/me/change-password", "alice",
		map[string]string{"current_password": "incorrect", "new_password": "a-better-password-2026"}, nil)
	mustStatus(t, status, 403, data)
	status, data = h.req(t, "GET", "/api/v1/me", "alice", nil, nil)
	mustStatus(t, status, 200, data)

	status, data = h.req(t, "POST", "/api/v1/me/change-password", "alice",
		map[string]string{"current_password": "super-long-password", "new_password": "super-long-password"}, nil)
	mustStatus(t, status, 400, data)

	status, data = h.req(t, "POST", "/api/v1/me/change-password", "alice",
		map[string]string{"current_password": "super-long-password", "new_password": "a-better-password-2026"}, nil)
	mustStatus(t, status, 200, data)
	if data["sessions_revoked"] != true {
		t.Fatalf("sessions were not revoked: %+v", data)
	}
	for _, token := range []string{firstToken, secondToken} {
		h.tokens["alice"] = token
		status, data = h.req(t, "GET", "/api/v1/me", "alice", nil, nil)
		mustStatus(t, status, 401, data)
	}
	status, data = h.req(t, "POST", "/api/v1/auth/login", "",
		map[string]string{"username": "alice", "password": "super-long-password", "mode": "api"}, nil)
	mustStatus(t, status, 401, data)
	h.login(t, "alice", "a-better-password-2026")
}

func TestAdminResetPasswordAndSessionRevocation(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	h.addUser(t, "alice")
	h.addUser(t, "bob")
	h.login(t, "alice", "super-long-password")
	h.login(t, "bob", "super-long-password")

	status, data := h.req(t, "POST", "/api/v1/admin/users/2/reset-password", "alice",
		map[string]string{"new_password": "new-password-for-alice"}, nil)
	mustStatus(t, status, 403, data)
	status, data = h.req(t, "POST", "/api/v1/admin/users/1/reset-password", "admin",
		map[string]string{"new_password": "admin-cannot-self-reset"}, nil)
	mustStatus(t, status, 409, data)
	status, data = h.req(t, "POST", "/api/v1/admin/users/2/reset-password", "admin",
		map[string]string{"new_password": "short"}, nil)
	mustStatus(t, status, 400, data)
	status, data = h.req(t, "POST", "/api/v1/admin/users/999/reset-password", "admin",
		map[string]string{"new_password": "valid-new-password"}, nil)
	mustStatus(t, status, 404, data)
	status, data = h.req(t, "POST", "/api/v1/admin/users/2/reset-password", "admin",
		map[string]string{"new_password": "valid-new-password"}, nil)
	mustStatus(t, status, 200, data)

	status, data = h.req(t, "GET", "/api/v1/me", "alice", nil, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "GET", "/api/v1/me", "bob", nil, nil)
	mustStatus(t, status, 200, data)
	h.login(t, "alice", "valid-new-password")
}

func TestBlockUserRevokesSessionsAndSourcePublishing(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	h.addUser(t, "alice")
	h.login(t, "alice", "super-long-password")
	id := h.channel(t, "alice", "ha/security")
	src := h.source(t, "alice")
	_, token := h.sourceToken(t, "alice", src, id)
	h.tokens["source"] = token
	payload := map[string]string{"channel": "ha/security", "title": "Door", "body": "Opened"}

	status, data := h.req(t, "POST", "/api/v1/messages", "source", payload, nil)
	mustStatus(t, status, 201, data)

	status, data = h.req(t, "PATCH", "/api/v1/admin/users/2", "alice",
		map[string]bool{"disabled": true}, nil)
	mustStatus(t, status, 403, data)
	status, data = h.req(t, "PATCH", "/api/v1/admin/users/1", "admin",
		map[string]bool{"disabled": true}, nil)
	mustStatus(t, status, 409, data)
	status, data = h.req(t, "PATCH", "/api/v1/admin/users/2", "admin",
		map[string]any{}, nil)
	mustStatus(t, status, 400, data)

	status, data = h.req(t, "PATCH", "/api/v1/admin/users/2", "admin",
		map[string]bool{"disabled": true}, nil)
	mustStatus(t, status, 200, data)
	if data["disabled"] != true {
		t.Fatalf("account not blocked: %+v", data)
	}
	status, data = h.req(t, "GET", "/api/v1/me", "alice", nil, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "POST", "/api/v1/messages", "source", payload, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "POST", "/api/v1/auth/login", "",
		map[string]string{"username": "alice", "password": "super-long-password", "mode": "api"}, nil)
	mustStatus(t, status, 401, data)

	status, data = h.req(t, "PATCH", "/api/v1/admin/users/2", "admin",
		map[string]bool{"disabled": false}, nil)
	mustStatus(t, status, 200, data)
	status, data = h.req(t, "POST", "/api/v1/messages", "source", payload, nil)
	mustStatus(t, status, 201, data)
	status, data = h.req(t, "GET", "/api/v1/me", "alice", nil, nil)
	mustStatus(t, status, 401, data)
	h.login(t, "alice", "super-long-password")
}

func TestPasswordsBoundedAndCookieOriginProtected(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	status, data := h.req(t, "POST", "/api/v1/admin/users", "admin",
		map[string]string{"username": "alice", "password": strings.Repeat("x", 129)}, nil)
	mustStatus(t, status, 400, data)
	status, data = h.req(t, "POST", "/api/v1/me/change-password", "admin",
		map[string]string{"current_password": "some-super-safe-password", "new_password": strings.Repeat("x", 129)}, nil)
	mustStatus(t, status, 400, data)
	status, data = h.req(t, "POST", "/api/v1/admin/users/999/reset-password", "admin",
		map[string]string{"new_password": strings.Repeat("x", 129)}, nil)
	mustStatus(t, status, 400, data)

	// Cookie mutations without Origin must never run, even for a valid session.
	status, data = h.req(t, "POST", "/api/v1/auth/login", "",
		map[string]string{"username": "admin", "password": "some-super-safe-password", "mode": "api"}, nil)
	mustStatus(t, status, 200, data)
	token := data["access_token"].(string)
	req, err := http.NewRequest(http.MethodPost, h.server.URL+"/api/v1/me/change-password",
		strings.NewReader(`{"current_password":"some-super-safe-password","new_password":"another-long-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "chimeport_session", Value: token})
	resp, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cookie-only missing Origin should be forbidden: %d", resp.StatusCode)
	}
}

func TestMultipleAdminsCannotBeLockedOutBySelfDisable(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	status, data := h.req(t, "POST", "/api/v1/admin/users", "admin",
		map[string]string{"username": "secondadmin", "password": "secondadmin-password", "role": "admin"}, nil)
	mustStatus(t, status, 201, data)
	h.login(t, "secondadmin", "secondadmin-password")
	status, data = h.req(t, "PATCH", "/api/v1/admin/users/2", "admin",
		map[string]bool{"disabled": true}, nil)
	mustStatus(t, status, 200, data)
	status, data = h.req(t, "PATCH", "/api/v1/admin/users/1", "admin",
		map[string]bool{"disabled": true}, nil)
	mustStatus(t, status, 409, data)
	// An unknown target is not implicitly created.
	status, data = h.req(t, "PATCH", "/api/v1/admin/users/"+strconv.Itoa(999), "admin",
		map[string]bool{"disabled": true}, nil)
	mustStatus(t, status, 404, data)
}
