package ws

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/trueos/sysup/defines"
	"log"
	"time"
)

type JSONReply struct {
	Method string `json:"method"`
	Info   string `json:"info"`
}

func SendMsg(msg string, msg_type ...string) {
	m_type := "info"

	if len(msg_type) > 0 {
		m_type = msg_type[0]
	}

	data := &JSONReply{
		Method: m_type,
		Info:   msg,
	}

	j_msg, err := json.Marshal(data)

	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}

	if err := defines.WSServer.WriteMessage(
		websocket.TextMessage, j_msg,
	); err != nil {
		log.Fatal(err)
	}
}

// Called when we want to signal that its time to close the WS connection
func CloseWs() {
	log.Println("Closing WS connection")
	log.Printf("closing ws")
	defer defines.WSServer.Close()
	defer defines.WSClient.Close()

	// Cleanly close the connection by sending a close message and then
	// waiting (with timeout) for the server to close the connection.
	c_err := defines.WSClient.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	)
	if c_err != nil {
		log.Println("write close:", c_err)
		return
	}
	s_err := defines.WSServer.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	)
	if s_err != nil {
		log.Println("write close:", s_err)
		return
	}
	time.Sleep(10 * time.Millisecond)
}
