package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebThemeAssets(t *testing.T) {
	tests := []struct {
		path      string
		mime      string
		contains  []string
	}{
		{"/", "text/html; charset=utf-8", []string{"/theme.js", "/themes.css", "/chimeport-icon.svg", "id=\"theme-select\"", "Примеры отправки"}},
		{"/examples", "text/html; charset=utf-8", []string{"/theme.js", "/themes.css", "/chimeport-icon.svg", "id=\"theme-select\""}},
		{"/theme.js", "text/javascript; charset=utf-8", []string{"localStorage", "DOMContentLoaded", "ocean", "paper", "control", "storage"}},
		{"/themes.css", "text/css; charset=utf-8", []string{"data-theme=\"ocean\"", "data-theme=\"paper\"", "data-theme=\"control\"", "@media"}},
		{"/chimeport-icon.svg", "image/svg+xml", []string{"<svg", "#8FDDCD"}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			resp := httptest.NewRecorder()
			(&App{}).web(resp, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if resp.Code != http.StatusOK {
				t.Fatalf("%s: got HTTP %d, want 200", tt.path, resp.Code)
			}
			if got := resp.Header().Get("Content-Type"); got != tt.mime {
				t.Fatalf("%s: wrong content type %q, want %q", tt.path, got, tt.mime)
			}
			if got := resp.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("%s: wrong cache control %q", tt.path, got)
			}
			for _, part := range tt.contains {
				if !strings.Contains(resp.Body.String(), part) {
					t.Errorf("%s: expected %q in response", tt.path, part)
				}
			}
		})
	}
	resp := httptest.NewRecorder()
	(&App{}).web(resp, httptest.NewRequest(http.MethodGet, "/theme-invalid.css", nil))
	if resp.Code != http.StatusNotFound {
		t.Fatalf("unknown static asset: got HTTP %d, want 404", resp.Code)
	}
}
