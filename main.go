package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	"github.com/pion/webrtc/v3"
)

// WebRTC and WebSocket configurations
var (
	upgrader = websocket.Upgrader{
		CheckOrigin:      func(r *http.Request) bool { return true },
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
	}

	webrtcConfig = webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}
)

// Global state management
var (
	broadcaster   *WebRTCConnection
	viewers       = make(map[string]*WebRTCConnection)
	viewersMutex  sync.RWMutex
)

// Types
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

// Server configuration
type ServerConfig struct {
	Port       string
	Host       string
	StunServer string
	StreamKey  string
}

func loadConfig() (*ServerConfig, error) {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found")
	}

	return &ServerConfig{
		Port:       getEnvOrDefault("WEBRTC_PORT", "8080"),
		Host:       getEnvOrDefault("SERVER_HOST", "localhost"),
		StunServer: getEnvOrDefault("STUN_SERVER", "stun:stun.l.google.com:19302"),
		StreamKey:  getEnvOrDefault("STREAM_KEY", "your-secret-stream-key"),
	}, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Middleware
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Route handlers
func setupRoutes(mux *http.ServeMux) {
	// Static files
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// WebSocket endpoints
	mux.HandleFunc("/broadcast", logRequest(HandleBroadcaster))
	mux.HandleFunc("/view", logRequest(HandleViewer))

	// Web routes
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/broadcast-status", handleBroadcastStatus)
}

func logRequest(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		handler(w, r)
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	view := r.URL.Query().Get("view")
	
	switch view {
	case "broadcaster":
		http.ServeFile(w, r, "static/broadcaster.html")
	case "viewer":
		http.ServeFile(w, r, "static/viewer.html")
	default:
		http.Redirect(w, r, "/?view=viewer", http.StatusFound)
	}
}

func handleBroadcastStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	json.NewEncoder(w).Encode(map[string]interface{}{
		"broadcasting": broadcaster != nil,
	})
}

// Main function
func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatal("Failed to load configuration:", err)
	}

	// Create router and setup routes
	mux := http.NewServeMux()
	setupRoutes(mux)

	// Wrap with middleware
	handler := corsMiddleware(mux)

	// Configure server
	server := &http.Server{
		Addr:    "0.0.0.0:" + config.Port,
		Handler: handler,
	}

	// Log configuration
	log.Printf("Starting server with configuration:")
	log.Printf("Port: %s", config.Port)
	log.Printf("Host: %s", config.Host)
	log.Printf("STUN Server: %s", config.StunServer)

	// Start server
	log.Printf("Server listening on port %s", config.Port)
	log.Fatal(server.ListenAndServe())
}

// Helper functions
func createPeerConnection() (*webrtc.PeerConnection, error) {
	return webrtc.NewPeerConnection(webrtcConfig)
}

func generateViewerID() string {
	return "viewer-" + string(os.Getpid())
} 