package main

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"aacpanel/web"
)

func assets(dir, prefix string) http.Handler {
	sub, err := fs.Sub(web.FS, dir)
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	return http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	}))
}

const panelName = "aacpanel"

func (s *Server) shellName() string {
	if name := strings.TrimSpace(s.hostName); name != "" {
		return name
	}
	return panelName
}

func (s *Server) manifestFile() http.Handler {
	body, err := web.FS.ReadFile("manifest.webmanifest")
	if err != nil {
		log.Printf("the frontend is not built: the manifest is unavailable (%v); build it with `go run ./cmd/webbuild`", err)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "the frontend is not built", http.StatusServiceUnavailable)
		})
	}
	named, err := renameManifest(body, s.shellName())
	if err != nil {
		log.Printf("the manifest was not renamed (%v); serving it as built", err)
		named = body
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if s.auth.Authorized(r) {
			w.Write(named)
			return
		}
		w.Write(body)
	})
}

func renameManifest(body []byte, name string) ([]byte, error) {
	var meta map[string]any
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, err
	}
	meta["name"] = name
	meta["short_name"] = name
	return json.Marshal(meta)
}

func embedFile(name, contentType string) http.Handler {
	body, err := web.FS.ReadFile(name)
	if err != nil {
		log.Printf("the frontend is not built: %s is unavailable (%v); build it with `go run ./cmd/webbuild`", name, err)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "the frontend is not built", http.StatusServiceUnavailable)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(body)
	})
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if s.auth.Authorized(r) {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	s.render(w, "app.html", map[string]any{"Host": panelName})
}

func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, "\\") {
		return "/app"
	}
	return next
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.auth.Clear(w, s.secure)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template %s: %v", name, err)
	}
}

func (s *Server) app(w http.ResponseWriter, r *http.Request) {
	s.render(w, "app.html", map[string]any{"Host": s.shellName()})
}
