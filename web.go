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
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Content-Type", "text/html")
		
		view := r.URL.Query().Get("view")
		
		data := PageData{
			StreamKey:     getStreamKey(),
			IsViewer:      view == "viewer",
			IsBroadcaster: view == "broadcaster",
			ServerWSURL:   getServerURL(),
		}

		tmpl.ExecuteTemplate(w, "index.html", data)
	})
} 