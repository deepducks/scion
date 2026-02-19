// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hub

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestWebServer(t *testing.T, cfg WebServerConfig) *WebServer {
	t.Helper()
	return NewWebServer(cfg)
}

func TestSPAShellHandler(t *testing.T) {
	ws := newTestWebServer(t, WebServerConfig{})

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	ws.Handler().ServeHTTP(rec, req)

	resp := rec.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	html := string(body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected Content-Type text/html, got %q", ct)
	}

	// Verify expected SPA shell elements
	checks := map[string]string{
		"__SCION_DATA__":  "hydration data script",
		"scion-app":       "root custom element",
		"main.js":         "client entry point script",
		"--scion-primary": "critical CSS variables",
		"scion-theme":     "theme detection script",
		shoelaceVersion:   "Shoelace CDN version",
	}
	for needle, desc := range checks {
		if !strings.Contains(html, needle) {
			t.Errorf("SPA shell missing %s (expected %q in HTML)", desc, needle)
		}
	}
}

func TestSPACatchAll(t *testing.T) {
	ws := newTestWebServer(t, WebServerConfig{})

	// Various SPA routes should all return the SPA shell
	paths := []string{"/", "/groves", "/agents", "/groves/abc123", "/settings", "/not-a-real-page"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			rec := httptest.NewRecorder()

			ws.Handler().ServeHTTP(rec, req)

			resp := rec.Result()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected status 200 for %s, got %d", path, resp.StatusCode)
			}

			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), "scion-app") {
				t.Errorf("expected SPA shell HTML for %s", path)
			}
		})
	}
}

func TestStaticAssetHandler_Disk(t *testing.T) {
	// Create a temporary directory with a test asset
	tmpDir := t.TempDir()
	testContent := "console.log('test');"
	if err := os.WriteFile(filepath.Join(tmpDir, "main.js"), []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test asset: %v", err)
	}

	ws := newTestWebServer(t, WebServerConfig{
		AssetsDir: tmpDir,
	})

	req := httptest.NewRequest("GET", "/assets/main.js", nil)
	rec := httptest.NewRecorder()

	ws.Handler().ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if string(body) != testContent {
		t.Errorf("expected %q, got %q", testContent, string(body))
	}

	// Non-hashed asset should get no-cache
	cc := resp.Header.Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("expected Cache-Control no-cache for non-hashed asset, got %q", cc)
	}
}

func TestStaticAssetHandler_HashedCaching(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "chunk-abc12345.js"), []byte("// chunk"), 0644); err != nil {
		t.Fatalf("failed to write test asset: %v", err)
	}

	ws := newTestWebServer(t, WebServerConfig{
		AssetsDir: tmpDir,
	})

	req := httptest.NewRequest("GET", "/assets/chunk-abc12345.js", nil)
	rec := httptest.NewRecorder()

	ws.Handler().ServeHTTP(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	cc := resp.Header.Get("Cache-Control")
	if cc != "public, max-age=86400" {
		t.Errorf("expected Cache-Control for hashed asset, got %q", cc)
	}
}

func TestStaticAssetHandler_NoAssets(t *testing.T) {
	ws := newTestWebServer(t, WebServerConfig{})

	req := httptest.NewRequest("GET", "/assets/main.js", nil)
	rec := httptest.NewRecorder()

	ws.Handler().ServeHTTP(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404 when no assets available, got %d", resp.StatusCode)
	}
}

func TestSecurityHeaders(t *testing.T) {
	ws := newTestWebServer(t, WebServerConfig{})

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	ws.Handler().ServeHTTP(rec, req)

	resp := rec.Result()

	expectedHeaders := map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"X-XSS-Protection":      "1; mode=block",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}

	for header, expected := range expectedHeaders {
		got := resp.Header.Get(header)
		if got != expected {
			t.Errorf("header %s: expected %q, got %q", header, expected, got)
		}
	}

	// Verify CSP is set and contains key directives
	csp := resp.Header.Get("Content-Security-Policy")
	if csp == "" {
		t.Error("Content-Security-Policy header not set")
	} else {
		cspChecks := []string{
			"default-src 'self'",
			"script-src 'self'",
			"cdn.jsdelivr.net",
			"fonts.googleapis.com",
			"fonts.gstatic.com",
		}
		for _, check := range cspChecks {
			if !strings.Contains(csp, check) {
				t.Errorf("CSP missing %q", check)
			}
		}
	}

	// Verify Permissions-Policy is set
	pp := resp.Header.Get("Permissions-Policy")
	if pp == "" {
		t.Error("Permissions-Policy header not set")
	} else if !strings.Contains(pp, "camera=()") {
		t.Errorf("Permissions-Policy missing camera restriction: %q", pp)
	}
}

func TestWebHealthz(t *testing.T) {
	ws := newTestWebServer(t, WebServerConfig{
		AssetsDir: "/tmp/test-assets",
	})

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()

	ws.Handler().ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	bodyStr := string(body)
	if !strings.Contains(bodyStr, `"status":"ok"`) {
		t.Errorf("expected status ok in response: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"component":"web"`) {
		t.Errorf("expected component web in response: %s", bodyStr)
	}
}

func TestIsHashedAsset(t *testing.T) {
	tests := []struct {
		path   string
		hashed bool
	}{
		{"chunk-abc12345.js", true},
		{"style-deadbeef.css", true},
		{"main.js", false},
		{"main.css", false},
		{"chunk-ab.js", false},       // hash too short
		{"chunk-ABCDEF12.js", true},   // uppercase hex
		{".js", false},               // no name
		{"no-extension", false},      // no extension
		{"name-ghijk.js", false},     // non-hex chars
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isHashedAsset(tt.path)
			if got != tt.hashed {
				t.Errorf("isHashedAsset(%q) = %v, want %v", tt.path, got, tt.hashed)
			}
		})
	}
}
