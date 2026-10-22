// Package ui serves the mocks capture UI: one page, a generated design-system
// stylesheet (assets/tz.css, see cssgen) and a small script. Everything is
// embedded; the page loads nothing from the network.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets
var assets embed.FS

// CSP allows only this origin; the favicon is an empty data: URL.
const csp = "default-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'"

func secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

// AssetsFS returns the embedded asset tree (tests).
func AssetsFS() fs.FS {
	sub, _ := fs.Sub(assets, "assets")
	return sub
}

// Index serves the page at "/".
func Index() http.Handler {
	page, err := fs.ReadFile(assets, "assets/index.html")
	if err != nil {
		panic("ui: index.html missing from embed: " + err.Error())
	}
	return secure(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	}))
}

// Assets serves /assets/*.
func Assets() http.Handler {
	return secure(http.StripPrefix("/assets/", http.FileServerFS(AssetsFS())))
}
