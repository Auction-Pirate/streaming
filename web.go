package main

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*
var templateFS embed.FS

type PageData struct {
	StreamKey    string
	IsViewer     bool
	IsBroadcaster bool
	ServerWSURL  string
}

func setupWebRoutes() {
	tmpl := template.Must(template.ParseFS(templateFS, "templates/*"))
	
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Add security headers
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:; media-src 'self' blob:")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=()")
		w.Header().Set("Content-Type", "text/html")
		
		view := r.URL.Query().Get("view")
		
		data := PageData{
			StreamKey:    getStreamKey(), // from your env
			IsViewer:     view == "viewer",
			IsBroadcaster: view == "broadcaster",
			ServerWSURL:  getServerURL(), // from your env
		}

		tmpl.ExecuteTemplate(w, "index.html", data)
	})
} 