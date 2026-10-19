// Package dashboard embeds the static Next.js export.
package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed out
var assets embed.FS

func Wrap(api http.Handler) http.Handler {
	sub, err := fs.Sub(assets, "out")
	if err != nil {
		return api
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/_next/") || strings.HasSuffix(r.URL.Path, ".html") || strings.HasSuffix(r.URL.Path, ".txt") {
			p := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
			if p == "." {
				p = "index.html"
			}
			if _, e := fs.Stat(sub, p); e == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		api.ServeHTTP(w, r)
	})
}
