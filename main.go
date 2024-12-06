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
	mux.HandleFunc("/broadcast", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received broadcast connection request from %s", r.RemoteAddr)
		handleBroadcaster(w, r)
	})

	mux.HandleFunc("/view", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received view connection request from %s", r.RemoteAddr)
		handleViewer(w, r)
	})

	// Web routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/index.html")
	})

	// Add this to your route handlers
	mux.HandleFunc("/broadcast-status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		
		json.NewEncoder(w).Encode(map[string]interface{}{
			"broadcasting": broadcaster != nil,
		})
	})

	// Wrap with CORS middleware
	handler := corsMiddleware(mux)

	// Get port from environment
	port := os.Getenv("WEBRTC_PORT")
	if port == "" {
		port = "8080"
	}

	// Add debug logging
	log.Printf("Starting server with configuration:")
	log.Printf("Port: %s", port)
	log.Printf("Host: %s", os.Getenv("SERVER_HOST"))
	log.Printf("STUN Server: %s", os.Getenv("STUN_SERVER"))

	server := &http.Server{
		Addr:    "0.0.0.0:" + port,
		Handler: handler,
	}

	log.Printf("Server listening on port %s", port)
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
	log.Printf("Received broadcast connection request from %s", r.RemoteAddr)
	
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("WebSocket connection established with broadcaster")

	pc, err := createPeerConnection()
	if err != nil {
		log.Printf("Failed to create peer connection: %v", err)
		return
	}
	defer pc.Close()

	// Create new broadcaster connection
	b := &WebRTCConnection{
		PeerConnection: pc,
		WebSocket:      conn,
	}

	// Set broadcaster
	broadcaster = b

	// Handle incoming messages
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			break
		}

		var message Message
		if err := json.Unmarshal(msg, &message); err != nil {
			log.Printf("Failed to parse message: %v", err)
			continue
		}

		log.Printf("Received message type: %s", message.Type)

		switch message.Type {
		case "offer":
			// Handle the offer
			err = pc.SetRemoteDescription(webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer,
				SDP:  message.SDP,
			})
			if err != nil {
				log.Printf("Failed to set remote description: %v", err)
				continue
			}

			// Create answer
			answer, err := pc.CreateAnswer(nil)
			if err != nil {
				log.Printf("Failed to create answer: %v", err)
				continue
			}

			// Set local description
			err = pc.SetLocalDescription(answer)
			if err != nil {
				log.Printf("Failed to set local description: %v", err)
				continue
			}

			// Send answer
			if err := conn.WriteJSON(Message{
				Type: "answer",
				SDP:  answer.SDP,
			}); err != nil {
				log.Printf("Failed to send answer: %v", err)
			}
		}
	}

	log.Printf("Broadcaster disconnected")
	broadcaster = nil
}

func handleViewer(w http.ResponseWriter, r *http.Request) {
	// Copy the HandleViewer function content from webrtc.go
	// ... (copy the entire function content)
}

func generateViewerID() string {
	return "viewer-" + string(os.Getpid())
} 