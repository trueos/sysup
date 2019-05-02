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

func SendInfoMsg(info string, bootloader ...bool) {
	data := &JSONReply{
		Method: "info",
		Info:   info,
	}

	if len(bootloader) > 0 {
		data.Method = "updatebootloader"
	}

	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	if err := defines.WSServer.WriteMessage(
		websocket.TextMessage, msg,
	); err != nil {
		log.Fatal(err)
	}
}

func SendShutdownMsg(info string) {
	data := &JSONReply{
		Method: "shutdown",
		Info:   info,
	}
	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	if err := defines.WSServer.WriteMessage(
		websocket.TextMessage, msg,
	); err != nil {
		log.Fatal(err)
	}
}

func SendFatalMsg(info string) {
	data := &JSONReply{
		Method: "fatal",
		Info:   info,
	}
	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	if err := defines.WSServer.WriteMessage(
		websocket.TextMessage, msg,
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
