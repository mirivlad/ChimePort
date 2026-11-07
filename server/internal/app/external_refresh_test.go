package app

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestExternalRefreshSupportsIdempotentRetryAndDeviceRevoke(t *testing.T) {
	h := setup(t)
	h.login(t, "admin", "some-super-safe-password")
	h.tokens["tms"] = createTrustedForTest(t, h, "tms")
	verifier := strings.Repeat("A", 43)
	sum := sha256.Sum256([]byte(verifier))
	status, data := h.req(t, "POST", "/api/v1/external/tickets", "tms", map[string]string{
		"recipient": "42", "device_id": strings.Repeat("a", 32),
		"code_challenge": base64.RawURLEncoding.EncodeToString(sum[:]),
	}, nil)
	mustStatus(t, status, 201, data)
	status, data = h.req(t, "POST", "/api/v1/external/exchange", "", map[string]string{
		"ticket": data["ticket"].(string), "code_verifier": verifier,
	}, nil)
	mustStatus(t, status, 200, data)
	oldAccess := data["access_token"].(string)
	oldRefresh := data["refresh_token"].(string)
	if data["expires_in"].(float64) != 900 {
		t.Fatal("access token lifetime should be 15 minutes")
	}
	retry := strings.Repeat("b", 32)
	payload := map[string]string{"refresh_token": oldRefresh, "retry_id": retry}
	status, data = h.req(t, "POST", "/api/v1/external/refresh", "", payload, nil)
	mustStatus(t, status, 200, data)
	newAccess := data["access_token"].(string)
	newRefresh := data["refresh_token"].(string)
	status, duplicate := h.req(t, "POST", "/api/v1/external/refresh", "", payload, nil)
	mustStatus(t, status, 200, duplicate)
	if duplicate["access_token"] != newAccess || duplicate["refresh_token"] != newRefresh {
		t.Fatal("retry must return the same pair")
	}
	h.tokens["old-access"] = oldAccess
	h.tokens["new-access"] = newAccess
	status, data = h.req(t, "GET", "/api/v1/external/info", "old-access", nil, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "GET", "/api/v1/external/info", "new-access", nil, nil)
	mustStatus(t, status, 200, data)
	if data["recipient"] != "42" {
		t.Fatal("refresh changed stream ownership")
	}
	status, data = h.req(t, "POST", "/api/v1/external/refresh", "", map[string]string{
		"refresh_token": oldRefresh, "retry_id": strings.Repeat("c", 32),
	}, nil)
	mustStatus(t, status, 401, data)
	status, data = h.req(t, "GET", "/api/v1/external/info", "new-access", nil, nil)
	mustStatus(t, status, 401, data)
}
