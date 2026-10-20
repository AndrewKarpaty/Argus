// Package web embeds the built single-page UI so that argusd ships as one
// binary serving both the API and the console.
//
// The UI is built separately (`make ui`), which means a fresh checkout can
// still `go build ./...` without Node installed: dist/ holds a placeholder,
// Handler reports that no build is present, and argusd logs how to produce
// one instead of failing to start.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var assets embed.FS

// Built reports whether an actual UI build is embedded.
func Built() bool {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		return false
	}
	_, err = fs.Stat(sub, "index.html")
	return err == nil
}

// Handler returns a handler for the embedded UI, and false when no build is
// embedded. Unknown paths fall back to index.html, because the client router
// owns every route the API does not.
func Handler() (http.Handler, bool) {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil, false
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, false
	}
	return &spa{files: sub, serve: http.FileServer(http.FS(sub)), index: index}, true
}

type spa struct {
	files fs.FS
	serve http.Handler
	index []byte
}

func (s *spa) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")

	// The entry document is written straight out rather than handed to the
	// file server, which would redirect "/index.html" to "./" and bounce the
	// fallback back into itself.
	if name == "" || name == "index.html" {
		s.writeIndex(w)
		return
	}

	info, err := fs.Stat(s.files, name)
	if err != nil || info.IsDir() {
		// A deep link such as /investigations/inv_123 is a client route, not a
		// file: hand the app its entry point and let it route.
		s.writeIndex(w)
		return
	}

	// Asset filenames carry a content hash, so they can be cached hard.
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	s.serve.ServeHTTP(w, r)
}

// writeIndex serves the entry document, which must never be cached: it names
// the hashed asset files of whichever build is deployed.
func (s *spa) writeIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(s.index)
}
