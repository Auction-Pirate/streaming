package main

import (
    "encoding/json"
    "log"
    "net/http"
    "os"

    "github.com/gorilla/websocket"
    "github.com/pion/webrtc/v3"
)

func handleWeb(w http.ResponseWriter, r *http.Request) {
    view := r.URL.Query().Get("view")
    
    // Serve the static index.html file with different configurations based on view
    if view == "broadcaster" || view == "viewer" {
        http.ServeFile(w, r, "static/index.html")
    } else {
        // Redirect to viewer by default
        http.Redirect(w, r, "/?view=viewer", http.StatusFound)
    }
}

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
        log.Printf("Got remote track: %v", remoteTrack.ID())
        
        // Create a local track to forward to viewers
        localTrack, err := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, remoteTrack.ID(), remoteTrack.StreamID())
        if err != nil {
            log.Printf("Failed to create local track: %v", err)
            return
        }
        b.StreamTracks = append(b.StreamTracks, localTrack)

        // Forward RTP packets from broadcaster to all viewers
        go func() {
            for {
                packet, _, err := remoteTrack.ReadRTP()
                if err != nil {
                    return
                }

                viewersMutex.RLock()
                for _, viewer := range viewers {
                    for _, track := range viewer.StreamTracks {
                        if track.ID() == localTrack.ID() {
                            _ = track.WriteRTP(packet)
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
            // Verify stream key
            if message.StreamKey != os.Getenv("STREAM_KEY") {
                log.Println("Invalid stream key")
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

    // Create viewer connection
    v := &WebRTCConnection{
        PeerConnection: pc,
        WebSocket:      conn,
    }

    // Add viewer to the map
    viewersMutex.Lock()
    viewers[viewerID] = v
    viewersMutex.Unlock()

    defer func() {
        viewersMutex.Lock()
        delete(viewers, viewerID)
        viewersMutex.Unlock()
    }()

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
            // Set remote description
            err = pc.SetRemoteDescription(webrtc.SessionDescription{
                Type: webrtc.SDPTypeOffer,
                SDP:  message.SDP,
            })
            if err != nil {
                log.Printf("Set remote desc error: %v", err)
                continue
            }

            // Add broadcaster tracks to viewer
            if broadcaster != nil {
                for _, track := range broadcaster.StreamTracks {
                    if _, err := pc.AddTrack(track); err != nil {
                        log.Printf("Add track error: %v", err)
                        continue
                    }
                }
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
}

func generateViewerID() string {
    return "viewer-" + string(os.Getpid())
} 