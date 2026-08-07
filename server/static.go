package server

import (
	"net/http"
	"os"
	"path/filepath"
)

// registerStaticUI serves the web UI from the web/ directory.
// It maps "/" to web/index.html so the signing page is available
// when users visit http://127.0.0.1:19711/
func (s *Server) registerStaticUI(mux *http.ServeMux) {
	webDir := s.findWebDir()

	if webDir == "" {
		s.logger.Printf("Web UI directory not found — static files disabled")
		return
	}

	// Serve index.html at root
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	})

	s.logger.Printf("Web UI available at / (from %s)", webDir)
}

// findWebDir locates the web/ directory relative to the executable or working dir.
func (s *Server) findWebDir() string {
	// Try relative to working directory
	if _, err := os.Stat("web/index.html"); err == nil {
		return "web"
	}

	// Try relative to executable
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "web")
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
			return dir
		}
	}

	return ""
}
