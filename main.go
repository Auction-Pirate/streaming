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
			return true // Allow all origins for demo purposes
		},
	}

	// Broadcaster's peer connection
	broadcasterPC *webrtc.PeerConnection
	// Mutex to protect broadcaster connection
	broadcasterLock sync.RWMutex
	// Map to store viewer peer connections
	viewers = make(map[string]*webrtc.PeerConnection)
	// Mutex to protect viewers map
	viewersLock sync.RWMutex
)

type WebRTCMessage struct {
	Type string `json:"type"`
	Data string `json:"data"`
	StreamKey string `json:"streamKey,omitempty"`
}

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}

	// Set up HTTP routes
	http.HandleFunc("/broadcast", handleBroadcaster)
	http.HandleFunc("/view", handleViewer)

	port := os.Getenv("WEBRTC_PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleBroadcaster(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Websocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	for {
		var msg WebRTCMessage
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("Error reading message: %v", err)
			break
		}

		// Verify stream key
		if msg.StreamKey != os.Getenv("STREAM_KEY") {
			log.Println("Invalid stream key")
			return
		}

		// Create WebRTC configuration
		config := webrtc.Configuration{
			ICEServers: []webrtc.ICEServer{
				{
					URLs: []string{os.Getenv("STUN_SERVER")},
				},
			},
		}

		if msg.Type == "offer" {
			broadcasterLock.Lock()
			var err error
			broadcasterPC, err = webrtc.NewPeerConnection(config)
			if err != nil {
				broadcasterLock.Unlock()
				log.Printf("Failed to create broadcaster peer connection: %v", err)
				return
			}

			// Set up tracks
			broadcasterPC.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
				// Forward the track to all viewers
				viewersLock.RLock()
				for _, viewer := range viewers {
					localTrack, err := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, remoteTrack.ID(), remoteTrack.StreamID())
					if err != nil {
						log.Printf("Failed to create local track: %v", err)
						continue
					}

					if _, err = viewer.AddTrack(localTrack); err != nil {
						log.Printf("Failed to add track to viewer: %v", err)
						continue
					}

					go func() {
						for {
							packet, _, err := remoteTrack.ReadRTP()
							if err != nil {
								return
							}
							if err := localTrack.WriteRTP(packet); err != nil {
								return
							}
						}
					}()
				}
				viewersLock.RUnlock()
			})

			// Set the remote description
			offer := webrtc.SessionDescription{}
			if err := json.Unmarshal([]byte(msg.Data), &offer); err != nil {
				broadcasterLock.Unlock()
				log.Printf("Failed to parse offer: %v", err)
				return
			}

			if err := broadcasterPC.SetRemoteDescription(offer); err != nil {
				broadcasterLock.Unlock()
				log.Printf("Failed to set remote description: %v", err)
				return
			}

			// Create answer
			answer, err := broadcasterPC.CreateAnswer(nil)
			if err != nil {
				broadcasterLock.Unlock()
				log.Printf("Failed to create answer: %v", err)
				return
			}

			if err := broadcasterPC.SetLocalDescription(answer); err != nil {
				broadcasterLock.Unlock()
				log.Printf("Failed to set local description: %v", err)
				return
			}

			broadcasterLock.Unlock()

			// Send answer back to broadcaster
			response := WebRTCMessage{
				Type: "answer",
				Data: answer.SDP,
			}
			if err := conn.WriteJSON(response); err != nil {
				log.Printf("Failed to send answer: %v", err)
				return
			}
		}
	}
}

func handleViewer(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Websocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Create a unique ID for this viewer
	viewerID := generateViewerID()

	for {
		var msg WebRTCMessage
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("Error reading message: %v", err)
			break
		}

		if msg.Type == "offer" {
			config := webrtc.Configuration{
				ICEServers: []webrtc.ICEServer{
					{
						URLs: []string{os.Getenv("STUN_SERVER")},
					},
				},
			}

			viewersLock.Lock()
			viewers[viewerID], err = webrtc.NewPeerConnection(config)
			if err != nil {
				viewersLock.Unlock()
				log.Printf("Failed to create viewer peer connection: %v", err)
				return
			}

			// Set the remote description
			offer := webrtc.SessionDescription{}
			if err := json.Unmarshal([]byte(msg.Data), &offer); err != nil {
				viewersLock.Unlock()
				log.Printf("Failed to parse offer: %v", err)
				return
			}

			if err := viewers[viewerID].SetRemoteDescription(offer); err != nil {
				viewersLock.Unlock()
				log.Printf("Failed to set remote description: %v", err)
				return
			}

			// Create answer
			answer, err := viewers[viewerID].CreateAnswer(nil)
			if err != nil {
				viewersLock.Unlock()
				log.Printf("Failed to create answer: %v", err)
				return
			}

			if err := viewers[viewerID].SetLocalDescription(answer); err != nil {
				viewersLock.Unlock()
				log.Printf("Failed to set local description: %v", err)
				return
			}

			viewersLock.Unlock()

			// Send answer back to viewer
			response := WebRTCMessage{
				Type: "answer",
				Data: answer.SDP,
			}
			if err := conn.WriteJSON(response); err != nil {
				log.Printf("Failed to send answer: %v", err)
				return
			}
		}
	}

	// Clean up viewer connection when they disconnect
	viewersLock.Lock()
	if pc, ok := viewers[viewerID]; ok {
		pc.Close()
		delete(viewers, viewerID)
	}
	viewersLock.Unlock()
}

func generateViewerID() string {
	// Simple implementation - you might want to use UUID in production
	return "viewer-" + string(os.Getpid())
} 