package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func (a *App) externalVapidPublic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(bearerToken(r), "cp_a_") {
		if _, ok := a.externalApp(w, r); !ok {
			return
		}
	} else {
		if _, ok := a.externalSession(w, r); !ok {
			return
		}
	}
	respond(w, 200, map[string]string{"public_key": a.vapidPublic})
}
func (a *App) externalWebPushSubscribe(w http.ResponseWriter, r *http.Request) {
	s, ok := a.externalSession(w, r)
	if !ok {
		return
	}
	var subscription webpush.Subscription
	if !decode(w, r, &subscription) {
		return
	}
	if err := validateBrowserKeys(subscription); err != nil {
		failure(w, 400, "invalid_subscription", err.Error())
		return
	}
	tx, err := a.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		a.err(w, err)
		return
	}
	defer tx.Rollback()
	var oldID, oldAppID int64
	var oldRecipient string
	err = tx.QueryRow("SELECT w.id,s.app_id,s.recipient FROM external_webpush w JOIN external_sessions s ON s.id=w.session_id WHERE w.endpoint=?", subscription.Endpoint).Scan(&oldID, &oldAppID, &oldRecipient)
	if err == nil && (oldAppID != s.app.id || oldRecipient != s.recipient) {
		failure(w, 409, "subscription_in_use", "This endpoint is subscribed to another recipient")
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		a.err(w, err)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		err = tx.QueryRow("SELECT COUNT(*) FROM external_webpush w JOIN external_sessions s ON s.id=w.session_id WHERE s.app_id=? AND s.recipient=? ", s.app.id, s.recipient).Scan(&count)
		if err != nil {
			a.err(w, err)
			return
		}
		if count >= maxWebPushSubscriptions {
			failure(w, 409, "subscription_limit", "Too many browser subscriptions")
			return
		}
		_, err = tx.Exec("INSERT INTO external_webpush(session_id,endpoint,p256dh,auth,created_at) VALUES(?,?,?,?,?)", s.id, subscription.Endpoint, subscription.Keys.P256dh, subscription.Keys.Auth, now())
	} else {
		_, err = tx.Exec("UPDATE external_webpush SET session_id=?,p256dh=?,auth=? WHERE id=?", s.id, subscription.Keys.P256dh, subscription.Keys.Auth, oldID)
	}
	if err != nil {
		a.err(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]bool{"subscribed": true})
}
func (a *App) externalWebPushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	s, ok := a.externalSession(w, r)
	if !ok {
		return
	}
	var in map[string]string
	if !decode(w, r, &in) {
		return
	}
	endpoint := in["endpoint"]
	if endpoint == "" || len(endpoint) > 2048 {
		failure(w, 400, "invalid_endpoint", "Invalid endpoint")
		return
	}
	_, err := a.Store.DB.ExecContext(r.Context(), "DELETE FROM external_webpush WHERE endpoint=? AND session_id IN (SELECT id FROM external_sessions WHERE app_id=? AND recipient=?)", endpoint, s.app.id, s.recipient)
	if err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]bool{"subscribed": false})
}
func (a *App) queueExternalPush(appID int64, recipient string, messageID int64, title, body, channel, priority, messageURL string) {
	if a.pushSlots == nil || a.pushSend == nil {
		return
	}
	select {
	case a.pushSlots <- struct{}{}:
		go func() {
			defer func() { <-a.pushSlots }()
			a.deliverExternalPush(appID, recipient, messageID, title, body, channel, priority, messageURL)
		}()
	default:
		slog.Warn("external web push worker saturated; event retained in history")
	}
}
func (a *App) deliverExternalPush(appID int64, recipient string, messageID int64, title, body, channel, priority, messageURL string) {
	rows, err := a.Store.DB.Query("SELECT w.id,w.endpoint,w.p256dh,w.auth FROM external_webpush w JOIN external_sessions s ON s.id=w.session_id JOIN external_apps a ON a.id=s.app_id WHERE s.app_id=? AND s.recipient=? AND a.disabled=0 ORDER BY w.id LIMIT ?", appID, recipient, maxWebPushSubscriptions)
	if err != nil {
		slog.Error("external push query failed", "error", err)
		return
	}
	type target struct {
		ID  int64
		sub webpush.Subscription
	}
	targets := []target{}
	for rows.Next() {
		var t target
		if err = rows.Scan(&t.ID, &t.sub.Endpoint, &t.sub.Keys.P256dh, &t.sub.Keys.Auth); err != nil {
			break
		}
		targets = append(targets, t)
	}
	if e := rows.Err(); e != nil {
		err = e
	}
	rows.Close()
	if err != nil {
		slog.Error("external push query failed", "error", err)
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"id": fmt.Sprint(messageID), "title": trimPushText(title, 120),
		"body": trimPushText(body, 300), "channel": channel, "priority": priority,
		"url": messageURL,
	})
	for _, t := range targets {
		if validateBrowserKeys(t.sub) != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
		status, sendErr := a.pushSend(ctx, payload, &t.sub)
		cancel()
		if sendErr != nil {
			slog.Warn("external web push delivery failed", "subscription_id", t.ID, "error_class", webPushErrorClass(sendErr))
			continue
		}
		if status == 404 || status == 410 {
			if _, err = a.Store.DB.Exec("DELETE FROM external_webpush WHERE id=? AND endpoint=?", t.ID, t.sub.Endpoint); err != nil {
				slog.Error("external push cleanup failed", "error", err)
			}
		} else if status < 200 || status >= 300 {
			slog.Warn("external push gateway refused delivery", "subscription_id", t.ID, "status", status)
		}
	}
}
