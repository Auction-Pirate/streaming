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
	mux.HandleFunc("/broadcast", logRequest(handleBroadcaster))
	mux.HandleFunc("/view", logRequest(handleViewer))

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

// Add the WebSocket handler functions here
func handleBroadcaster(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	defer conn.Close()

	pc, err := createPeerConnection()
	if err != nil {
		log.Printf("Create PC error: %v", err)
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

	// Handle incoming tracks
	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("Got remote track from broadcaster: %v, kind: %v", remoteTrack.ID(), remoteTrack.Kind())
		
		// Create a local track to forward to viewers
		localTrack, err := webrtc.NewTrackLocalStaticRTP(
			remoteTrack.Codec().RTPCodecCapability,
			"audio", // Fixed ID for audio track
			"audio", // Fixed stream ID
		)
		if err != nil {
			log.Printf("Failed to create local track: %v", err)
			return
		}
		b.StreamTracks = append(b.StreamTracks, localTrack)
		log.Printf("Created local track for forwarding: %v", localTrack.ID())

		// Forward RTP packets from broadcaster to all viewers
		go func() {
			for {
				packet, _, err := remoteTrack.ReadRTP()
				if err != nil {
					log.Printf("Failed to read RTP packet: %v", err)
					return
				}

				viewersMutex.RLock()
				for id, viewer := range viewers {
					if len(viewer.StreamTracks) > 0 {
						if err := viewer.StreamTracks[0].WriteRTP(packet); err != nil {
							log.Printf("Failed to write RTP to viewer %s: %v", id, err)
						}
					}
				}
				viewersMutex.RUnlock()
			}
		}()
	})

	// Handle incoming messages
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Read error: %v", err)
			break
		}

		var message Message
		if err := json.Unmarshal(msg, &message); err != nil {
			log.Printf("Parse error: %v", err)
			continue
		}

		switch message.Type {
		case "offer":
			// Verify stream key with better logging
			expectedKey := os.Getenv("STREAM_KEY")
			if message.StreamKey != expectedKey {
				log.Printf("Invalid stream key. Expected: %s, Got: %s", expectedKey, message.StreamKey)
				return
			}

			// Set remote description
			err = pc.SetRemoteDescription(webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer,
				SDP:  message.SDP,
			})
			if err != nil {
				log.Printf("Set remote desc error: %v", err)
				continue
			}

			// Create answer
			answer, err := pc.CreateAnswer(nil)
			if err != nil {
				log.Printf("Create answer error: %v", err)
				continue
			}

			// Set local description
			err = pc.SetLocalDescription(answer)
			if err != nil {
				log.Printf("Set local desc error: %v", err)
				continue
			}

			// Send answer
			resp := Message{
				Type: "answer",
				SDP:  answer.SDP,
			}
			if err := conn.WriteJSON(resp); err != nil {
				log.Printf("Write error: %v", err)
			}
		}
	}

	// Clean up
	broadcaster = nil
}

func handleViewer(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	defer conn.Close()

	pc, err := createPeerConnection()
	if err != nil {
		log.Printf("Create PC error: %v", err)
		return
	}
	defer pc.Close()

	// Generate viewer ID
	viewerID := generateViewerID()
	log.Printf("New viewer connected: %s", viewerID)

	// Create viewer connection
	v := &WebRTCConnection{
		PeerConnection: pc,
		WebSocket:      conn,
	}

	// Add viewer to the map BEFORE processing offer
	viewersMutex.Lock()
	viewers[viewerID] = v
	viewersMutex.Unlock()

	defer func() {
		viewersMutex.Lock()
		delete(viewers, viewerID)
		
		viewersMutex.Unlock()
		log.Printf("Viewer disconnected: %s", viewerID)
	}()

	// Add broadcaster tracks to viewer if broadcaster exists
	if broadcaster != nil {
		log.Printf("Adding %d tracks from broadcaster to viewer %s", 
			len(broadcaster.StreamTracks), viewerID)
		
		for _, track := range broadcaster.StreamTracks {
			rtpSender, err := pc.AddTrack(track)
			if err != nil {
				log.Printf("Failed to add track to viewer %s: %v", viewerID, err)
				continue
			}
			log.Printf("Added track %s to viewer %s", track.ID(), viewerID)

			// Handle RTP packets
			go func() {
				rtcpBuf := make([]byte, 1500)
				for {
					if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
						return
					}
				}
			}()
		}
	} else {
		log.Printf("No broadcaster present for viewer %s", viewerID)
	}

	// Handle incoming messages
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Read error from viewer %s: %v", viewerID, err)
			break
		}

		var message Message
		if err := json.Unmarshal(msg, &message); err != nil {
			log.Printf("Parse error from viewer %s: %v", viewerID, err)
			continue
		}

		switch message.Type {
		case "offer":
			log.Printf("Received offer from viewer %s", viewerID)
			
			offer := webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer,
				SDP:  message.SDP,
			}

			if err := pc.SetRemoteDescription(offer); err != nil {
				log.Printf("Failed to set remote description for viewer %s: %v", viewerID, err)
				continue
			}
			log.Printf("Set remote description for viewer %s", viewerID)

			answer, err := pc.CreateAnswer(nil)
			if err != nil {
				log.Printf("Failed to create answer for viewer %s: %v", viewerID, err)
				continue
			}

			if err := pc.SetLocalDescription(answer); err != nil {
				log.Printf("Failed to set local description for viewer %s: %v", viewerID, err)
				continue
			}

			resp := Message{
				Type: "answer",
				SDP:  answer.SDP,
			}
			if err := conn.WriteJSON(resp); err != nil {
				log.Printf("Failed to send answer to viewer %s: %v", viewerID, err)
			}
			log.Printf("Sent answer to viewer %s", viewerID)
		}
	}
} 