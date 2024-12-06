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

// Basic WebRTC configuration
var webrtcConfig = webrtc.Configuration{
	ICEServers: []webrtc.ICEServer{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
	},
}

// WebSocket upgrader
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Types
type WebRTCConnection struct {
	PeerConnection *webrtc.PeerConnection
	WebSocket      *websocket.Conn
	AudioTrack     *webrtc.TrackLocalStaticRTP
}

type Message struct {
	Type      string `json:"type"`
	SDP       string `json:"sdp,omitempty"`
	StreamKey string `json:"streamKey,omitempty"`
}

// Global state
var (
	broadcaster   *WebRTCConnection
	viewers       = make(map[string]*WebRTCConnection)
	viewersMutex  sync.RWMutex
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found")
	}

	// Setup routes
	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/broadcast", handleBroadcaster)
	http.HandleFunc("/view", handleViewer)
	http.HandleFunc("/broadcast-status", handleBroadcastStatus)

	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// Start server
	port := os.Getenv("WEBRTC_PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server starting on port %s", port)
	log.Fatal(http.ListenAndServe("0.0.0.0:"+port, nil))
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
	json.NewEncoder(w).Encode(map[string]bool{"broadcasting": broadcaster != nil})
}

func handleBroadcaster(w http.ResponseWriter, r *http.Request) {
	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Create peer connection
	pc, err := webrtc.NewPeerConnection(webrtcConfig)
	if err != nil {
		log.Printf("Failed to create peer connection: %v", err)
		return
	}
	defer pc.Close()

	// Create broadcaster connection
	b := &WebRTCConnection{
		PeerConnection: pc,
		WebSocket:      conn,
	}
	broadcaster = b
	defer func() { broadcaster = nil }()

	// Handle incoming audio track
	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		log.Printf("Received audio track from broadcaster")

		// Create a track to forward to viewers
		localTrack, err := webrtc.NewTrackLocalStaticRTP(
			remoteTrack.Codec().RTPCodecCapability,
			"audio",
			"broadcast",
		)
		if err != nil {
			log.Printf("Failed to create local track: %v", err)
			return
		}
		b.AudioTrack = localTrack

		// Forward RTP packets to viewers
		for {
			packet, _, err := remoteTrack.ReadRTP()
			if err != nil {
				return
			}

			viewersMutex.RLock()
			for _, viewer := range viewers {
				if viewer.AudioTrack != nil {
					if err := viewer.AudioTrack.WriteRTP(packet); err != nil {
						log.Printf("Failed to forward RTP packet: %v", err)
					}
				}
			}
			viewersMutex.RUnlock()
		}
	})

	// Handle signaling
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("Failed to read message: %v", err)
			return
		}

		switch msg.Type {
		case "offer":
			// Verify stream key
			if msg.StreamKey != os.Getenv("STREAM_KEY") {
				log.Printf("Invalid stream key")
				return
			}

			// Set remote description
			if err := pc.SetRemoteDescription(webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer,
				SDP:  msg.SDP,
			}); err != nil {
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
			if err := pc.SetLocalDescription(answer); err != nil {
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
}

func handleViewer(w http.ResponseWriter, r *http.Request) {
	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Create peer connection
	pc, err := webrtc.NewPeerConnection(webrtcConfig)
	if err != nil {
		log.Printf("Failed to create peer connection: %v", err)
		return
	}
	defer pc.Close()

	// Generate viewer ID
	viewerID := "viewer-" + string(os.Getpid())

	// Create viewer connection
	v := &WebRTCConnection{
		PeerConnection: pc,
		WebSocket:      conn,
	}

	// Add viewer to map
	viewersMutex.Lock()
	viewers[viewerID] = v
	viewersMutex.Unlock()

	// Cleanup on exit
	defer func() {
		viewersMutex.Lock()
		delete(viewers, viewerID)
		viewersMutex.Unlock()
	}()

	// Add broadcaster's track if broadcasting
	if broadcaster != nil && broadcaster.AudioTrack != nil {
		if _, err := pc.AddTrack(broadcaster.AudioTrack); err != nil {
			log.Printf("Failed to add audio track: %v", err)
			return
		}
		log.Printf("Added broadcaster's audio track to viewer %s", viewerID)
	}

	// Handle signaling
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("Failed to read message: %v", err)
			return
		}

		switch msg.Type {
		case "offer":
			// Set remote description
			if err := pc.SetRemoteDescription(webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer,
				SDP:  msg.SDP,
			}); err != nil {
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
			if err := pc.SetLocalDescription(answer); err != nil {
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
} 