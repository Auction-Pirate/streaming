package main

import (
    "encoding/json"
    "log"
    "net/http"

    "github.com/gorilla/websocket"
    "github.com/pion/webrtc/v3"
)

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