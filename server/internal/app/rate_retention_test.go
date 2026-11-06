package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestPublishRateLimitAndIdempotentReplay(t *testing.T) {
	h := setup(t)
	h.api.PublishRatePerMinute = 2
	h.login(t, "admin", "some-super-safe-password")
	ch := h.channel(t, "admin", "ha/alerts")
	source := h.source(t, "admin")
	_, first := h.sourceToken(t, "admin", source, ch)
	_, second := h.sourceToken(t, "admin", source, ch)
	h.tokens["first"] = first
	h.tokens["second"] = second
	message := map[string]string{"channel": "ha/alerts", "title": "Door", "body": "Open"}
	status, data := h.req(t, "POST", "/api/v1/messages", "first", message, map[string]string{"Idempotency-Key": "door-1"})
	mustStatus(t, status, 201, data)
	status, data = h.req(t, "POST", "/api/v1/messages", "first", message, map[string]string{"Idempotency-Key": "door-1"})
	mustStatus(t, status, 200, data)
	if data["duplicate"] != true {
		t.Fatalf("expected idempotent replay: %v", data)
	}
	status, data = h.req(t, "POST", "/api/v1/messages", "first", message, map[string]string{"Idempotency-Key": "door-2"})
	mustStatus(t, status, 201, data)
	status, data = h.req(t, "POST", "/api/v1/messages", "first", message, map[string]string{"Idempotency-Key": "door-3"})
	mustStatus(t, status, 429, data)
	if data["error"].(map[string]any)["code"] != "rate_limited" {
		t.Fatalf("wrong rate limit error: %+v", data)
	}
	payload, _ := json.Marshal(message)
	req, err := http.NewRequest("POST", h.server.URL+"/api/v1/messages", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+first)
	resp, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	retry, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if resp.StatusCode != 429 || err != nil || retry < 1 || retry > 60 {
		t.Fatalf("429 must supply Retry-After: status=%d header=%q err=%v", resp.StatusCode, resp.Header.Get("Retry-After"), err)
	}
	status, data = h.req(t, "POST", "/api/v1/messages", "second", message, nil)
	mustStatus(t, status, 201, data)
	var count int
	if err := h.store.DB.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count); err != nil || count != 3 {
		t.Fatalf("rate limiting should persist only three messages: %v count=%d", err, count)
	}
}

func TestConcurrentPublishRateIsAtomic(t *testing.T) {
	h := setup(t)
	h.api.PublishRatePerMinute = 5
	h.login(t, "admin", "some-super-safe-password")
	ch := h.channel(t, "admin", "monitor/events")
	source := h.source(t, "admin")
	_, token := h.sourceToken(t, "admin", source, ch)
	h.tokens["publish"] = token
	var wg sync.WaitGroup
	statuses := make(chan int, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, _ := h.req(t, "POST", "/api/v1/messages", "publish", map[string]string{"channel": "monitor/events", "title": fmt.Sprintf("Event %d", i)}, map[string]string{"Idempotency-Key": fmt.Sprintf("batch-%d", i)})
			statuses <- code
		}(i)
	}
	wg.Wait()
	close(statuses)
	var accepted, rejected int
	for status := range statuses {
		switch status {
		case 201:
			accepted++
		case 429:
			rejected++
		default:
			t.Errorf("unexpected status %d", status)
		}
	}
	if accepted != 5 || rejected != 7 {
		t.Fatalf("non-atomic quota: %d accepted, %d rejected", accepted, rejected)
	}
}

func TestRetentionPrunesDependenciesAndSignalsGapPerOwner(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	h.addUser(t, "bob")
	h.login(t, "bob", "super-long-password")
	aliceChannel := h.channel(t, "admin", "system/alerts")
	bobChannel := h.channel(t, "bob", "personal/alerts")
	aliceSource := h.source(t, "admin")
	bobSource := h.source(t, "bob")
	_, aliceToken := h.sourceToken(t, "admin", aliceSource, aliceChannel)
	_, bobToken := h.sourceToken(t, "bob", bobSource, bobChannel)
	h.tokens["alice-publish"] = aliceToken
	h.tokens["bob-publish"] = bobToken
	_, aInitial := h.req(t, "GET", "/api/v1/messages", "admin", nil, nil)
	_, bInitial := h.req(t, "GET", "/api/v1/messages", "bob", nil, nil)
	aCursor := aInitial["next_cursor"].(string)
	bCursor := bInitial["next_cursor"].(string)
	status, old := h.req(t, "POST", "/api/v1/messages", "alice-publish", map[string]string{"channel": "system/alerts", "title": "Old"}, map[string]string{"Idempotency-Key": "old-event"})
	mustStatus(t, status, 201, old)
	oldID, _ := strconv.ParseInt(old["id"].(string), 10, 64)
	status, data := h.req(t, "POST", "/api/v1/messages/"+old["id"].(string)+"/read", "admin", nil, nil)
	mustStatus(t, status, 200, data)
	oldTime := time.Now().UTC().Add(-40 * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := h.store.DB.Exec("UPDATE messages SET created_at=? WHERE id=?", oldTime, oldID); err != nil {
		t.Fatal(err)
	}
	status, data = h.req(t, "POST", "/api/v1/messages", "alice-publish", map[string]string{"channel": "system/alerts", "title": "Recent"}, nil)
	mustStatus(t, status, 201, data)
	status, data = h.req(t, "POST", "/api/v1/messages", "bob-publish", map[string]string{"channel": "personal/alerts", "title": "Bob"}, nil)
	mustStatus(t, status, 201, data)
	n, err := h.store.PruneExpired(context.Background(), 30, 100)
	if err != nil || n != 1 {
		t.Fatalf("prune result %d err=%v", n, err)
	}
	for _, name := range []string{"idempotency", "read_states"} {
		var count int
		if err := h.store.DB.QueryRow("SELECT COUNT(*) FROM " + name).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s still references deleted message: count=%d err=%v", name, count, err)
		}
	}
	status, data = h.req(t, "GET", "/api/v1/messages?after="+aCursor, "admin", nil, nil)
	mustStatus(t, status, 200, data)
	if data["retention_gap"] != true || len(data["items"].([]any)) != 1 {
		t.Fatalf("owner should see sync gap and recent item: %+v", data)
	}
	status, data = h.req(t, "GET", "/api/v1/messages?after="+bCursor, "bob", nil, nil)
	mustStatus(t, status, 200, data)
	if data["retention_gap"] != false || len(data["items"].([]any)) != 1 {
		t.Fatalf("other owner must not see a gap: %+v", data)
	}
	n, err = h.store.PruneExpired(context.Background(), 30, 100)
	if err != nil || n != 0 {
		t.Fatalf("cleanup is not idempotent: %d %v", n, err)
	}
}

func TestUpgradeV2ToV4(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"DROP TABLE publication_windows",
		"DROP TABLE retention_watermarks",
		"DROP TABLE webpush_subscriptions",
		"DROP INDEX messages_created_at_idx",
		"PRAGMA user_version=2",
	} {
		if _, err := store.DB.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	store.DB.Close()
	upgraded, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("v2 migration failed: %v", err)
	}
	defer upgraded.DB.Close()
	var version int
	if err := upgraded.DB.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatalf("unexpected schema %d: %v", version, err)
	}
}
