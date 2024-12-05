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
		view := r.URL.Query().Get("view")
		
		data := PageData{
			StreamKey:    getStreamKey(), // from your env
			IsViewer:     view == "viewer",
			IsBroadcaster: view == "broadcaster",
			ServerWSURL:  getServerURL(), // from your env
		}

		w.Header().Set("Content-Type", "text/html")
		tmpl.ExecuteTemplate(w, "index.html", data)
	})
} 