package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type harness struct {
	store  *Store
	server *httptest.Server
	tokens map[string]string
	dir    string
}

func setup(t *testing.T) *harness {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Bootstrap("admin", "some-super-safe-password"); err != nil {
		t.Fatal(err)
	}
	var handler http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	api, err := New(store, ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	handler = api.Handler()
	t.Cleanup(func() { ts.Close(); store.DB.Close() })
	return &harness{store: store, server: ts, tokens: map[string]string{}, dir: dir}
}
func (h *harness) req(t *testing.T, method, path, user string, payload any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var body *bytes.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	r, err := http.NewRequest(method, h.server.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if token := h.tokens[user]; token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	resp, err := h.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, data
}
func mustStatus(t *testing.T, status, want int, result map[string]any) {
	t.Helper()
	if status != want {
		t.Fatalf("status %d expected %d: %+v", status, want, result)
	}
}
func (h *harness) login(t *testing.T, username, password string) {
	t.Helper()
	status, data := h.req(t, "POST", "/api/v1/auth/login", "", map[string]any{"username": username, "password": password, "mode": "api"}, nil)
	mustStatus(t, status, 200, data)
	h.tokens[username] = data["access_token"].(string)
}
func (h *harness) addUser(t *testing.T, username string) {
	t.Helper()
	status, data := h.req(t, "POST", "/api/v1/admin/users", "admin", map[string]any{"username": username, "password": "super-long-password", "role": "user"}, nil)
	mustStatus(t, status, 201, data)
}
func (h *harness) channel(t *testing.T, user, slug string) int64 {
	t.Helper()
	status, data := h.req(t, "POST", "/api/v1/channels", user, map[string]any{"slug": slug, "name": slug, "description": "test"}, nil)
	mustStatus(t, status, 201, data)
	return int64(data["id"].(float64))
}
func (h *harness) source(t *testing.T, user string) int64 {
	t.Helper()
	status, data := h.req(t, "POST", "/api/v1/sources", user, map[string]string{"name": "home-assistant", "description": "test"}, nil)
	mustStatus(t, status, 201, data)
	return int64(data["id"].(float64))
}
func (h *harness) sourceToken(t *testing.T, user string, sourceID, channelID int64) (int64, string) {
	t.Helper()
	status, data := h.req(t, "POST", fmt.Sprintf("/api/v1/sources/%d/tokens", sourceID), user, map[string]any{"label": "door", "description": "door sensor", "channel_ids": []int64{channelID}}, nil)
	mustStatus(t, status, 201, data)
	return int64(data["id"].(float64)), data["token"].(string)
}
func TestEndToEndDurabilityAndIsolation(t *testing.T) {
	h := setup(t)
	if err := h.store.Bootstrap("second", "some-super-safe-password"); err == nil {
		t.Fatal("bootstrap must only work once")
	}
	h.login(t, "admin", "some-super-safe-password")
	h.addUser(t, "alice")
	h.addUser(t, "bob")
	h.login(t, "alice", "super-long-password")
	h.login(t, "bob", "super-long-password")
	ch := h.channel(t, "alice", "home/security")
	bobCh := h.channel(t, "bob", "home/security")
	sourceID := h.source(t, "alice")
	_, sourceToken := h.sourceToken(t, "alice", sourceID, ch)
	h.tokens["source"] = sourceToken
	status, data := h.req(t, "GET", "/api/v1/channels", "bob", nil, nil)
	mustStatus(t, status, 200, data)
	items := data["items"].([]any)
	if len(items) != 1 || int64(items[0].(map[string]any)["id"].(float64)) != bobCh {
		t.Fatal("Bob must see only his channel")
	}
	status, data = h.req(t, "POST", fmt.Sprintf("/api/v1/sources/%d/tokens", sourceID), "bob", map[string]any{"label": "bad", "channel_ids": []int64{bobCh}}, nil)
	mustStatus(t, status, 404, data)
	status, data = h.req(t, "POST", fmt.Sprintf("/api/v1/sources/%d/tokens", sourceID), "alice", map[string]any{"label": "bad", "channel_ids": []int64{bobCh}}, nil)
	mustStatus(t, status, 400, data)
	status, data = h.req(t, "POST", "/api/v1/messages", "alice", map[string]any{"channel": "home/security", "title": "Door", "body": "open"}, nil)
	mustStatus(t, status, 401, data)
	msg := map[string]any{"channel": "home/security", "title": "Door", "body": "Open", "priority": "high", "tags": []string{"sensor"}, "url": "https://example.org/door"}
	headers := map[string]string{"Idempotency-Key": "key-001"}
	status, data = h.req(t, "POST", "/api/v1/messages", "source", msg, headers)
	mustStatus(t, status, 201, data)
	messageID := data["id"].(string)
	status, data = h.req(t, "POST", "/api/v1/messages", "source", msg, headers)
	mustStatus(t, status, 200, data)
	if data["id"] != messageID {
		t.Fatal("idempotency returned another id")
	}
	msg["body"] = "different"
	status, data = h.req(t, "POST", "/api/v1/messages", "source", msg, headers)
	mustStatus(t, status, 409, data)
	status, data = h.req(t, "GET", "/api/v1/messages", "bob", nil, nil)
	mustStatus(t, status, 200, data)
	if len(data["items"].([]any)) != 0 {
		t.Fatal("Bob saw Alice's messages")
	}
	status, data = h.req(t, "GET", "/api/v1/messages", "admin", nil, nil)
	mustStatus(t, status, 200, data)
	if len(data["items"].([]any)) != 0 {
		t.Fatal("Admin saw private messages")
	}
	status, data = h.req(t, "GET", "/api/v1/messages", "alice", nil, nil)
	mustStatus(t, status, 200, data)
	if len(data["items"].([]any)) != 1 {
		t.Fatal("message missing or duplicate")
	}
	cursor := data["next_cursor"].(string)
	status, data = h.req(t, "GET", "/api/v1/messages?after="+cursor, "bob", nil, nil)
	mustStatus(t, status, 400, data)
	status, data = h.req(t, "GET", "/api/v1/messages?after="+cursor, "alice", nil, nil)
	mustStatus(t, status, 200, data)
	if len(data["items"].([]any)) != 0 {
		t.Fatal("cursor repeat")
	}
	status, data = h.req(t, "POST", "/api/v1/messages/"+messageID+"/read", "bob", nil, nil)
	mustStatus(t, status, 404, data)
	status, data = h.req(t, "POST", "/api/v1/messages/"+messageID+"/read", "alice", nil, nil)
	mustStatus(t, status, 200, data)
	status, data = h.req(t, "GET", "/api/v1/messages", "alice", nil, nil)
	mustStatus(t, status, 200, data)
	if data["items"].([]any)[0].(map[string]any)["read_at"] == nil {
		t.Fatal("read state not saved")
	}
	// Reopen the same database after the server stopped: messages, tokens and instance ID survive.
	id := h.store.InstanceID
	h.server.Close()
	h.store.DB.Close()
	reopened, err := OpenStore(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.DB.Close()
	if id != reopened.InstanceID {
		t.Fatal("instance ID changed after restart")
	}
	var count int
	if err := reopened.DB.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count); err != nil || count != 1 {
		t.Fatalf("persistence failed: %v count %d", err, count)
	}
}
func TestTokenRevocationAndMethods(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	channelID := h.channel(t, "admin", "server/backup")
	sourceID := h.source(t, "admin")
	tokenID, token := h.sourceToken(t, "admin", sourceID, channelID)
	h.tokens["token"] = token
	body := map[string]string{"channel": "server/backup", "title": "Backup", "body": "OK"}
	status, data := h.req(t, "POST", "/api/v1/messages", "token", body, nil)
	mustStatus(t, status, 201, data)
	status, data = h.req(t, "GET", "/api/v1/admin/users", "token", nil, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "GET", "/api/v1/messages", "token", nil, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "DELETE", "/api/v1/sources/"+strconv.FormatInt(sourceID, 10)+"/tokens/"+strconv.FormatInt(tokenID, 10), "admin", nil, nil)
	mustStatus(t, status, 200, data)
	status, data = h.req(t, "POST", "/api/v1/messages", "token", body, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "POST", "/api/v1/messages", "admin", body, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "GET", "/api/v1/sources/"+strconv.FormatInt(sourceID, 10)+"/tokens", "admin", nil, nil)
	mustStatus(t, status, 200, data)
	record := data["items"].([]any)[0].(map[string]any)
	if record["revoked_at"] == nil {
		t.Fatal("revocation metadata missing")
	}
	if _, hasSecret := record["token"]; hasSecret {
		t.Fatal("token was leaked in metadata")
	}
}
func TestWebCookieOrigin(t *testing.T) {
	h := setup(t)
	status, data := h.req(t, "POST", "/api/v1/auth/login", "", map[string]string{"username": "admin", "password": "some-super-safe-password", "mode": "web"}, nil)
	mustStatus(t, status, 403, data)
	r, err := http.NewRequest("POST", h.server.URL+"/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"some-super-safe-password","mode":"web"}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", h.server.URL)
	response, err := h.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("same-origin web login failed")
	}
	var cookie *http.Cookie
	for _, c := range response.Cookies() {
		if c.Name == "chimeport_session" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie not hardened")
	}
	makeRequest := func(origin string) int {
		req, _ := http.NewRequest("POST", h.server.URL+"/api/v1/channels", strings.NewReader(`{"slug":"security","name":"Security"}`))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := h.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := makeRequest("https://evil.example"); got != 403 {
		t.Fatalf("origin bypass: %d", got)
	}
	if got := makeRequest(h.server.URL); got != 201 {
		t.Fatalf("same-origin mutation rejected: %d", got)
	}
}
func TestParallelPublish(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	ch := h.channel(t, "admin", "server/jobs")
	src := h.source(t, "admin")
	_, token := h.sourceToken(t, "admin", src, ch)
	h.tokens["token"] = token
	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := map[string]string{"channel": "server/jobs", "title": fmt.Sprintf("Job %d", i), "body": "done"}
			status, data := h.req(t, "POST", "/api/v1/messages", "token", body, map[string]string{"Idempotency-Key": fmt.Sprintf("job-%d", i)})
			if status != 201 {
				errs <- fmt.Errorf("status=%d data=%v", status, data)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var count int
	if err := h.store.DB.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count); err != nil || count != n {
		t.Fatalf("parallel publish lost messages: %v %d", err, count)
	}
}

func TestSourceTokenCanBeShownAgainOnlyToOwner(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	h.addUser(t, "alice")
	h.addUser(t, "bob")
	h.login(t, "alice", "super-long-password")
	h.login(t, "bob", "super-long-password")
	ch := h.channel(t, "alice", "home/sensor")
	src := h.source(t, "alice")
	tokenID, token := h.sourceToken(t, "alice", src, ch)
	path := fmt.Sprintf("/api/v1/sources/%d/tokens/%d/secret", src, tokenID)
	status, data := h.req(t, "GET", path, "alice", nil, nil)
	mustStatus(t, status, 200, data)
	if data["token"] != token {
		t.Fatal("token cannot be retrieved a second time")
	}
	status, data = h.req(t, "GET", path, "bob", nil, nil)
	mustStatus(t, status, 404, data)
	status, data = h.req(t, "GET", path, "", nil, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "GET", fmt.Sprintf("/api/v1/sources/%d/tokens", src), "alice", nil, nil)
	mustStatus(t, status, 200, data)
	if _, exists := data["items"].([]any)[0].(map[string]any)["token"]; exists {
		t.Fatal("token must be hidden from list response")
	}
	h.store.DB.Close()
	reopened, err := OpenStore(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.DB.Close()
	restored, err := reopened.getTokenSecret(tokenID)
	if err != nil || restored != token {
		t.Fatalf("token was not recoverable after restart: %v", err)
	}
}
