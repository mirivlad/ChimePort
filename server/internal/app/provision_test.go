package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProvisionIntegrationScopedTokenAndPrivateFile(t *testing.T) {
	h := setup(t)
	tokenPath := filepath.Join(t.TempDir(), "ha-token.txt")
	n, err := h.store.ProvisionSource(context.Background(), "admin", "home-assistant", "ha", []ProvisionChannel{
		{Slug: "home/door", Name: "Door"},
		{Slug: "home/weather", Name: "Weather"},
	}, tokenPath)
	if err != nil || n <= 0 {
		t.Fatalf("provision: %d %v", n, err)
	}
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("token file permissions %o", info.Mode().Perm())
	}
	secret, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	h.tokens["ha-source"] = string(secret[:len(secret)-1])
	status, data := h.req(t, "POST", "/api/v1/messages", "ha-source", map[string]string{"channel": "home/door", "title": "Door opened"}, nil)
	mustStatus(t, status, 201, data)
	status, data = h.req(t, "POST", "/api/v1/messages", "ha-source", map[string]string{"channel": "no/access", "title": "Wrong"}, nil)
	mustStatus(t, status, 403, data)
	if _, err = h.store.ProvisionSource(context.Background(), "admin", "home-assistant", "ha", []ProvisionChannel{{Slug: "home/door", Name: "Door"}}, tokenPath); err == nil {
		t.Fatal("should refuse replacing existing token file")
	}
	var count int
	if err := h.store.DB.QueryRow("SELECT count(*) FROM source_tokens WHERE source_id=(SELECT id FROM sources WHERE name='home-assistant')").Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate provisioning persisted despite existing file: %d %v", count, err)
	}
}

func TestProvisionRejectsDisabledOrMissingOwner(t *testing.T) {
	h := setup(t)
	if _, err := h.store.ProvisionSource(context.Background(), "unknown", "source", "token", []ProvisionChannel{{Slug: "home/test", Name: "Test"}}, filepath.Join(t.TempDir(), "token")); err == nil {
		t.Fatal("missing user should be rejected")
	}
}
