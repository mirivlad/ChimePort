package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrustedApplicationProvisionedPrivately(t *testing.T) {
	h := setup(t)
	path := filepath.Join(t.TempDir(), "tms-secret")
	id, err := h.store.ProvisionTrustedApplication(context.Background(), "tms", "https://tasks.example.org", path)
	if err != nil || id < 1 {
		t.Fatalf("provision: %d %v", id, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("private file: %o", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(token, "cp_a_") {
		t.Fatal("not trusted app token")
	}
	h.tokens["trusted"] = token
	status, data := h.req(t, "POST", "/api/v1/external/messages", "trusted", map[string]string{"recipient": "42", "title": "Task assigned"}, nil)
	mustStatus(t, status, 201, data)
	if _, err = h.store.ProvisionTrustedApplication(context.Background(), "tms", "https://tasks.example.org", path); err == nil {
		t.Fatal("duplicate provisioning must fail")
	}
	var count int
	if err = h.store.DB.QueryRow("SELECT count(*) FROM external_apps WHERE slug='tms'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("provision duplicate persisted: %d %v", count, err)
	}
}
