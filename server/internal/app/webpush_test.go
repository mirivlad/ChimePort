package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"github.com/mirivlad/ChimePort/server/internal/maintenance"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func browserTestSubscription(t *testing.T) webpush.Subscription {
	t.Helper()
	_, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err = rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	return webpush.Subscription{
		Endpoint: "https://fcm.googleapis.com/fcm/send/fake-browser-test-endpoint",
		Keys:     webpush.Keys{P256dh: pub, Auth: base64.RawURLEncoding.EncodeToString(auth)},
	}
}

func TestWebPushEndpointValidation(t *testing.T) {
	for _, endpoint := range []string{
		"https://fcm.googleapis.com/fcm/send/browser-key",
		"https://updates.push.services.mozilla.com/wpush/v2/browser-key",
		"https://web.push.apple.com/push/browser-key",
		"https://a.push.apple.com/foo",
		"https://wns2.example.notify.windows.com/send",
	} {
		if err := validatePushEndpoint(endpoint); err != nil {
			t.Errorf("valid %q: %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{
		"http://fcm.googleapis.com/fcm/send/key",
		"https://127.0.0.1/private",
		"https://localhost/private",
		"https://evil-fcm.googleapis.com/private",
		"https://web.push.apple.com.evil.invalid/private",
		"https://user:pass@web.push.apple.com/private",
		"https://web.push.apple.com:8443/private",
		"https://web.push.apple.com/private#fragment",
		"file:///etc/passwd",
	} {
		if validatePushEndpoint(endpoint) == nil {
			t.Errorf("accepted unsafe endpoint %q", endpoint)
		}
	}
}

func TestWebPushMigrationFromV3AndStableVAPIDKeys(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := New(store, "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	if first.vapidPublic == "" {
		t.Fatal("no public key")
	}
	var encrypted string
	if err := store.DB.QueryRow("SELECT value FROM instance WHERE key='webpush_vapid_private'").Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, first.vapidPublic) || len(encrypted) < 50 {
		t.Fatal("VAPID private material not encrypted in SQLite")
	}
	if _, err := store.DB.Exec("DROP TABLE webpush_subscriptions"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec("PRAGMA user_version=3"); err != nil {
		t.Fatal(err)
	}
	store.DB.Close()

	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.DB.Close()
	var version int
	if err := reopened.DB.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatalf("migration to schema 4: %d %v", version, err)
	}
	second, err := New(reopened, "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	if first.vapidPublic != second.vapidPublic {
		t.Fatal("VAPID key changed across reopen/migration")
	}
}

func TestWebPushSubscriptionSessionIsolationAndLogout(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	subscription := browserTestSubscription(t)
	status, data := h.req(t, "GET", "/api/v1/webpush/vapid-public-key", "admin", nil, nil)
	mustStatus(t, status, 200, data)
	if _, err := base64.RawURLEncoding.DecodeString(data["public_key"].(string)); err != nil {
		t.Fatal(err)
	}
	status, data = h.req(t, "POST", "/api/v1/webpush/subscriptions", "admin", subscription, nil)
	mustStatus(t, status, 200, data)
	status, data = h.req(t, "POST", "/api/v1/webpush/subscriptions", "admin", subscription, nil)
	mustStatus(t, status, 200, data) // idempotent refresh
	status, data = h.req(t, "GET", "/api/v1/webpush/subscriptions", "admin", nil, nil)
	mustStatus(t, status, 200, data)
	if data["count"].(float64) != 1 {
		t.Fatalf("duplicate subscription: %v", data)
	}

	h.addUser(t, "alice")
	h.login(t, "alice", "super-long-password")
	status, data = h.req(t, "GET", "/api/v1/webpush/subscriptions", "alice", nil, nil)
	mustStatus(t, status, 200, data)
	if data["count"].(float64) != 0 {
		t.Fatal("another account can see subscription")
	}
	status, data = h.req(t, "POST", "/api/v1/webpush/subscriptions", "alice", subscription, nil)
	if status != 409 {
		t.Fatalf("cross-user endpoint takeover allowed: %d, %v", status, data)
	}

	bad := subscription
	bad.Endpoint = "https://127.0.0.1/internal"
	status, _ = h.req(t, "POST", "/api/v1/webpush/subscriptions", "alice", bad, nil)
	if status != 400 {
		t.Fatalf("unsafe endpoint status %d", status)
	}

	// Logging out deletes the session; ON DELETE CASCADE prevents delayed
	// notifications even when the browser has not yet unsubscribed locally.
	status, data = h.req(t, "POST", "/api/v1/auth/logout", "admin", nil, nil)
	mustStatus(t, status, 200, data)
	var count int
	if err := h.store.DB.QueryRow("SELECT COUNT(*) FROM webpush_subscriptions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("subscriptions remained after logout: %d (%v)", count, err)
	}
	status, _ = h.req(t, "GET", "/api/v1/webpush/vapid-public-key", "admin", nil, nil)
	if status != 401 {
		t.Fatalf("logged out public key status: %d", status)
	}
}

func TestWebPushSendAfterCommitAndEndpointExpiry(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	subscription := browserTestSubscription(t)
	status, data := h.req(t, "POST", "/api/v1/webpush/subscriptions", "admin", subscription, nil)
	mustStatus(t, status, 200, data)
	channel := h.channel(t, "admin", "servers")
	source := h.source(t, "admin")
	_, token := h.sourceToken(t, "admin", source, channel)
	h.tokens["publisher"] = token
	sent := make(chan map[string]string, 1)
	h.api.pushSend = func(_ context.Context, payload []byte, _ *webpush.Subscription) (int, error) {
		var output map[string]string
		if err := json.Unmarshal(payload, &output); err != nil {
			return 0, err
		}
		var count int
		if err := h.store.DB.QueryRow("SELECT count(*) FROM messages").Scan(&count); err != nil || count < 1 {
			t.Errorf("push happened before database commit: %d %v", count, err)
		}
		sent <- output
		return 201, nil
	}
	status, data = h.req(t, "POST", "/api/v1/messages", "publisher", map[string]string{
		"channel": "servers", "title": "Backup done", "body": "Everything saved", "priority": "high",
	}, nil)
	mustStatus(t, status, 201, data)
	select {
	case payload := <-sent:
		if payload["title"] != "Backup done" || payload["priority"] != "high" || payload["id"] == "" {
			t.Fatalf("incorrect push payload: %v", payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("web push was not queued after publish")
	}

	h.api.pushSend = func(_ context.Context, _ []byte, _ *webpush.Subscription) (int, error) {
		return 410, nil // push provider revoked subscription
	}
	h.api.deliverWebPush(1, 123, "Test", "body", "servers", "normal")
	var count int
	err := h.store.DB.QueryRow("SELECT count(*) FROM webpush_subscriptions").Scan(&count)
	if err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expired endpoint retained: %d", count)
	}
}

func TestPWAAssetsServedWithoutCachingAPI(t *testing.T) {
	h := setup(t)
	for path, mime := range map[string]string{
		"/sw.js":                "text/javascript",
		"/push.js":              "text/javascript",
		"/manifest.webmanifest": "application/manifest+json",
		"/icons/icon-192.png":   "image/png",
		"/icons/icon-512.png":   "image/png",
	} {
		res, err := h.server.Client().Get(h.server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), mime) {
			t.Errorf("%s: HTTP %d, mime %q", path, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
}

func TestWebPushBackupRestoresVAPIDIdentity(t *testing.T) {
	from := filepath.Join(t.TempDir(), "source")
	s, err := OpenStore(from)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(s, "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	pub := a.vapidPublic
	instance := s.InstanceID
	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	// Maintenance operates on an independent SQLite snapshot and token-key.
	// Closing the live SQLite connection is not required.
	manifest, err := maintenance.Backup(from, archive)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 5 {
		t.Fatalf("wrong backup schema: %d", manifest.SchemaVersion)
	}
	if _, err = maintenance.Verify(archive); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "restored")
	if err := os.Mkdir(restored, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = maintenance.Restore(archive, restored, instance); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()
	rs, err := OpenStore(restored)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.DB.Close()
	next, err := New(rs, "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	if next.vapidPublic != pub || rs.InstanceID != instance {
		t.Fatal("VAPID key or server identity changed after restore")
	}
}
