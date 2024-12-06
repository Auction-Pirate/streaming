package main

import (
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	"github.com/pion/webrtc/v3"
)

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	// Global state
	broadcaster     *WebRTCConnection
	viewers         = make(map[string]*WebRTCConnection)
	viewersMutex    sync.RWMutex
)

type WebRTCConnection struct {
	PeerConnection *webrtc.PeerConnection
	WebSocket      *websocket.Conn
	StreamTracks   []*webrtc.TrackLocalStaticRTP
}

type Message struct {
	Type      string `json:"type"`
	SDP       string `json:"sdp,omitempty"`
	StreamKey string `json:"streamKey,omitempty"`
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found")
	}

	// CORS middleware
	corsMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Set CORS headers
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			// Handle preflight requests
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	// Create router
	mux := http.NewServeMux()

	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// WebSocket endpoints
	mux.HandleFunc("/broadcast", handleBroadcaster)
	mux.HandleFunc("/view", handleViewer)

	// Web routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/index.html")
	})

	// Wrap with CORS middleware
	handler := corsMiddleware(mux)

	// Get port from environment
	port := os.Getenv("WEBRTC_PORT")
	if port == "" {
		port = "8080"
	}

	// Start server
	log.Printf("Server starting on :%s", port)
	log.Printf("Access broadcaster at: http://%s:%s/?view=broadcaster", os.Getenv("SERVER_HOST"), port)
	log.Printf("Access viewer at: http://%s:%s/?view=viewer", os.Getenv("SERVER_HOST"), port)
	
	server := &http.Server{
		Addr:    ":" + port,
		Handler: handler,
	}

	log.Fatal(server.ListenAndServe())
}

func createPeerConnection() (*webrtc.PeerConnection, error) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}
	return webrtc.NewPeerConnection(config)
}

// Add the handler functions here
func handleBroadcaster(w http.ResponseWriter, r *http.Request) {
	// Copy the HandleBroadcaster function content from webrtc.go
	// ... (copy the entire function content)
}

func handleViewer(w http.ResponseWriter, r *http.Request) {
	// Copy the HandleViewer function content from webrtc.go
	// ... (copy the entire function content)
}

func generateViewerID() string {
	return "viewer-" + string(os.Getpid())
} 