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
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ptone/scion-agent/web"
)

// shoelaceVersion is the Shoelace CDN version used by the SPA shell.
const shoelaceVersion = "2.19.0"

// WebServerConfig holds configuration for the web frontend server.
type WebServerConfig struct {
	// Port is the HTTP port to listen on (default 8080).
	Port int
	// Host is the address to bind to (e.g., "0.0.0.0").
	Host string
	// AssetsDir overrides embedded assets with a filesystem directory.
	// When set, static files are served from this path instead of the embedded FS.
	AssetsDir string
	// Debug enables verbose debug logging.
	Debug bool
}

// WebServer serves the web frontend SPA shell and static assets.
type WebServer struct {
	config      WebServerConfig
	httpServer  *http.Server
	mux         *http.ServeMux
	assets      fs.FS     // embedded or nil
	assetsDisk  string    // filesystem override path, or ""
	shellTmpl   *template.Template
}

// spaShellTemplate is the Go html/template for the SPA shell page.
// It mirrors the structure from web/src/server/ssr/templates.ts but renders
// a client-only shell (no SSR content).
var spaShellTemplate = `<!DOCTYPE html>
<html lang="en" data-theme="light">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Scion</title>

    <!-- Preconnect to CDNs for faster loading -->
    <link rel="preconnect" href="https://cdn.jsdelivr.net">
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>

    <!-- Fonts -->
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
    <link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">

    <!-- Shoelace Component Library -->
    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/@shoelace-style/shoelace@{{.ShoelaceVersion}}/cdn/themes/light.css">
    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/@shoelace-style/shoelace@{{.ShoelaceVersion}}/cdn/themes/dark.css" media="(prefers-color-scheme: dark)">
    <script type="module" src="https://cdn.jsdelivr.net/npm/@shoelace-style/shoelace@{{.ShoelaceVersion}}/cdn/shoelace-autoloader.js"></script>

    <!-- Initial state for hydration -->
    <script id="__SCION_DATA__" type="application/json">{}</script>

    <style>
        /* Critical CSS - Core layout to prevent FOUC */

        /* Color Palette - Light Mode (inlined for fast first paint) */
        :root {
            /* Primary */
            --scion-primary-50: #eff6ff;
            --scion-primary-500: #3b82f6;
            --scion-primary-600: #2563eb;
            --scion-primary-700: #1d4ed8;

            /* Neutral */
            --scion-neutral-50: #f8fafc;
            --scion-neutral-100: #f1f5f9;
            --scion-neutral-200: #e2e8f0;
            --scion-neutral-500: #64748b;
            --scion-neutral-600: #475569;
            --scion-neutral-700: #334155;
            --scion-neutral-800: #1e293b;
            --scion-neutral-900: #0f172a;

            /* Semantic */
            --scion-primary: var(--scion-primary-500);
            --scion-primary-hover: var(--scion-primary-600);
            --scion-bg: var(--scion-neutral-50);
            --scion-bg-subtle: var(--scion-neutral-100);
            --scion-surface: #ffffff;
            --scion-text: var(--scion-neutral-800);
            --scion-text-muted: var(--scion-neutral-500);
            --scion-border: var(--scion-neutral-200);

            /* Layout */
            --scion-sidebar-width: 260px;
            --scion-header-height: 60px;

            /* Typography */
            --scion-font-sans: 'Inter', ui-sans-serif, system-ui, -apple-system, sans-serif;
            --scion-font-mono: 'JetBrains Mono', ui-monospace, monospace;
        }

        /* Dark mode support */
        @media (prefers-color-scheme: dark) {
            :root:not([data-theme="light"]) {
                --scion-primary: #60a5fa;
                --scion-primary-hover: #93c5fd;
                --scion-bg: var(--scion-neutral-900);
                --scion-bg-subtle: var(--scion-neutral-800);
                --scion-surface: var(--scion-neutral-800);
                --scion-text: #f1f5f9;
                --scion-text-muted: #94a3b8;
                --scion-border: var(--scion-neutral-700);
            }
        }

        [data-theme="dark"] {
            --scion-primary: #60a5fa;
            --scion-primary-hover: #93c5fd;
            --scion-bg: var(--scion-neutral-900);
            --scion-bg-subtle: var(--scion-neutral-800);
            --scion-surface: var(--scion-neutral-800);
            --scion-text: #f1f5f9;
            --scion-text-muted: #94a3b8;
            --scion-border: var(--scion-neutral-700);
        }

        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }

        html, body {
            height: 100%;
            font-family: var(--scion-font-sans);
            background: var(--scion-bg);
            color: var(--scion-text);
            -webkit-font-smoothing: antialiased;
            -moz-osx-font-smoothing: grayscale;
        }

        #app {
            min-height: 100%;
        }

        /* Prevent FOUC for custom elements */
        scion-app:not(:defined),
        scion-nav:not(:defined),
        scion-header:not(:defined),
        scion-breadcrumb:not(:defined),
        scion-status-badge:not(:defined),
        scion-page-home:not(:defined),
        scion-page-groves:not(:defined),
        scion-page-agents:not(:defined),
        scion-page-404:not(:defined) {
            display: block;
            opacity: 0.5;
        }

        /* Shoelace component loading state */
        sl-button:not(:defined),
        sl-icon:not(:defined),
        sl-badge:not(:defined),
        sl-drawer:not(:defined),
        sl-dropdown:not(:defined),
        sl-menu:not(:defined),
        sl-menu-item:not(:defined),
        sl-breadcrumb:not(:defined),
        sl-breadcrumb-item:not(:defined),
        sl-tooltip:not(:defined),
        sl-avatar:not(:defined) {
            visibility: hidden;
        }
    </style>

    <!-- Theme detection script (runs before paint) -->
    <script>
        (function() {
            var saved = localStorage.getItem('scion-theme');
            if (saved === 'dark' || (!saved && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
                document.documentElement.setAttribute('data-theme', 'dark');
                document.documentElement.classList.add('sl-theme-dark');
            }
        })();
    </script>
</head>
<body>
    <div id="app"><scion-app></scion-app></div>

    <!-- Client entry point -->
    <script type="module" src="/assets/main.js"></script>
</body>
</html>`

// spaShellData holds the template data for the SPA shell.
type spaShellData struct {
	ShoelaceVersion string
}

// NewWebServer creates a new web frontend server.
func NewWebServer(cfg WebServerConfig) *WebServer {
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}

	ws := &WebServer{
		config: cfg,
		mux:    http.NewServeMux(),
	}

	// Resolve asset source
	if cfg.AssetsDir != "" {
		ws.assetsDisk = cfg.AssetsDir
		slog.Info("Web server using filesystem assets", "dir", cfg.AssetsDir)
	} else if web.AssetsEmbedded {
		sub, err := fs.Sub(web.ClientAssets, "dist/client")
		if err != nil {
			slog.Error("Failed to create sub-filesystem from embedded assets", "error", err)
		} else {
			ws.assets = sub
		}
		slog.Info("Web server using embedded assets")
	} else {
		slog.Warn("No web assets available: build with embedded assets or use --web-assets-dir")
	}

	// Parse SPA shell template
	tmpl, err := template.New("spa-shell").Parse(spaShellTemplate)
	if err != nil {
		slog.Error("Failed to parse SPA shell template", "error", err)
	}
	ws.shellTmpl = tmpl

	ws.registerRoutes()

	return ws
}

// registerRoutes sets up the web server routes.
func (ws *WebServer) registerRoutes() {
	ws.mux.HandleFunc("/healthz", ws.handleHealthz)
	ws.mux.Handle("/assets/", ws.staticHandler())
	ws.mux.HandleFunc("/", ws.spaHandler())
}

// handleHealthz returns the web server health status.
func (ws *WebServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok","component":"web","assetsDir":"%s","assetsEmbedded":%t}`,
		ws.config.AssetsDir, web.AssetsEmbedded)
}

// staticHandler returns an http.Handler that serves static assets.
func (ws *WebServer) staticHandler() http.Handler {
	if ws.assetsDisk == "" && ws.assets == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "no assets available", http.StatusNotFound)
		})
	}

	var fileServer http.Handler
	if ws.assetsDisk != "" {
		fileServer = http.FileServer(http.Dir(ws.assetsDisk))
	} else {
		fileServer = http.FileServer(http.FS(ws.assets))
	}

	return http.StripPrefix("/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set cache headers based on whether the filename contains a hash.
		// Vite hashed assets (e.g., chunk-abc123.js) get long-lived caching.
		// Non-hashed entry points (e.g., main.js) get revalidation.
		if isHashedAsset(r.URL.Path) {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	}))
}

// isHashedAsset checks if a path looks like it contains a content hash.
// Vite produces filenames like "chunk-abc12345.js" or "style-abc12345.css".
func isHashedAsset(path string) bool {
	// Look for the pattern: name-<hash>.ext where hash is hex chars
	lastDot := strings.LastIndex(path, ".")
	if lastDot <= 0 {
		return false
	}
	name := path[:lastDot]
	lastDash := strings.LastIndex(name, "-")
	if lastDash <= 0 || lastDash >= len(name)-1 {
		return false
	}
	hash := name[lastDash+1:]
	if len(hash) < 6 {
		return false
	}
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// spaHandler returns the SPA shell HTML for any route not matched by other handlers.
func (ws *WebServer) spaHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		if ws.shellTmpl == nil {
			http.Error(w, "SPA shell template not available", http.StatusInternalServerError)
			return
		}

		data := spaShellData{
			ShoelaceVersion: shoelaceVersion,
		}
		if err := ws.shellTmpl.Execute(w, data); err != nil {
			slog.Error("Failed to render SPA shell", "error", err)
		}
	}
}

// securityHeadersMiddleware adds security headers to all responses.
func (ws *WebServer) securityHeadersMiddleware(next http.Handler) http.Handler {
	// Build CSP matching the Koa server's policy (web/src/server/config.ts:154-162)
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://cdn.webawesome.com",
		"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://cdn.webawesome.com https://fonts.googleapis.com",
		"font-src 'self' https://fonts.gstatic.com https://cdn.jsdelivr.net https://cdn.webawesome.com",
		"img-src 'self' data: https:",
		"connect-src 'self' ws: wss: http://localhost:* http://127.0.0.1:*",
	}, "; ")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()")
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs incoming requests.
func (ws *WebServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		if ws.config.Debug || wrapped.statusCode >= 400 {
			slog.Info("Web request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.statusCode),
				slog.Duration("duration", time.Since(start)),
			)
		}
	})
}

// Start starts the web frontend HTTP server.
func (ws *WebServer) Start(ctx context.Context) error {
	handler := ws.securityHeadersMiddleware(ws.mux)
	handler = ws.loggingMiddleware(handler)

	ws.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", ws.config.Host, ws.config.Port),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	slog.Info("Web frontend server starting", "host", ws.config.Host, "port", ws.config.Port)

	errCh := make(chan error, 1)
	go func() {
		if err := ws.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ws.Shutdown(context.Background())
	}
}

// Shutdown gracefully shuts down the web server.
func (ws *WebServer) Shutdown(ctx context.Context) error {
	if ws.httpServer == nil {
		return nil
	}

	slog.Info("Web frontend server shutting down...")

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return ws.httpServer.Shutdown(ctx)
}

// Handler returns the HTTP handler for testing without starting a listener.
func (ws *WebServer) Handler() http.Handler {
	handler := ws.securityHeadersMiddleware(ws.mux)
	handler = ws.loggingMiddleware(handler)
	return handler
}
