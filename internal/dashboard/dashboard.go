// Package dashboard embeds the static Farm.js dashboard export.
package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Embed all static assets including dot/underscore directories such as _farm.
//
//go:embed all:out
var assets embed.FS

func isStaticDashboardRequest(requestPath string) bool {
	if requestPath == "/" ||
		requestPath == "/favicon.svg" ||
		requestPath == "/farm-client.js" ||
		requestPath == "/farm-client.css" ||
		requestPath == "/farm-client-manifest.json" {
		return true
	}
	if strings.HasPrefix(requestPath, "/assets/") ||
		strings.HasPrefix(requestPath, "/chunks/") ||
		strings.HasPrefix(requestPath, "/fonts/") ||
		strings.HasPrefix(requestPath, "/_farm/") {
		return true
	}
	if strings.HasPrefix(requestPath, "/farm-client-") &&
		(strings.HasSuffix(requestPath, ".js") || strings.HasSuffix(requestPath, ".css")) {
		return true
	}
	return strings.HasSuffix(requestPath, ".html") || strings.HasSuffix(requestPath, ".txt")
}

func Wrap(api http.Handler) http.Handler {
	sub, err := fs.Sub(assets, "out")
	if err != nil {
		return api
	}
	files := http.FileServer(http.FS(sub))
	routes := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isStaticDashboardRequest(r.URL.Path) {
			p := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
			if p == "." {
				p = "index.html"
			}
			if info, e := fs.Stat(sub, p); e == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		api.ServeHTTP(w, r)
	})
	return newCookieAuth(routes)
}
