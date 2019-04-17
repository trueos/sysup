package main

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/trueos/sysup/defines"
	"log"
	"net/http"
)

func readws(w http.ResponseWriter, r *http.Request) {
	var err error
	defines.Conns, err = defines.Updater.Upgrade(w, r, nil)
	if err != nil {
		log.Print("update:", err)
		return
	}
	defer defines.Conns.Close()
	for {
		_, message, err := defines.Conns.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		if !json.Valid(message) {
			log.Println("INVALID JSON")
			continue

		}

		// Start decoding the incoming JSON
		var env defines.Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			sendinfomsg("Invalid JSON received")
			log.Println("Warning: Invalid JSON message received")
			log.Println(err)
		}
		switch env.Method {
		case "check":
			checkforupdates()
		case "listtrains":
			dotrainlist()
		case "settrain":
			dosettrain(message)
		case "update":
			doupdate(message)
		case "updatebootloader":
			updateloader("")
			sendblmsg("Finished boot-loader process")
		default:
			log.Println("Uknown JSON Method:", env.Method)
		}

		// log.Printf("server-recv: %s", message)
		//err = defines.Conns.WriteMessage(mt, message)
		//if err != nil {
		//	log.Println("write:", err)
		//	break
		//}
	}
}

func sendblmsg(info string) {
	type JSONReply struct {
		Method string `json:"method"`
		Info   string `json:"info"`
	}

	data := &JSONReply{
		Method: "updatebootloader",
		Info:   info,
	}
	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	if err := defines.Conns.WriteMessage(websocket.TextMessage, msg); err != nil {
		log.Fatal(err)
	}
}

func sendinfomsg(info string) {
	type JSONReply struct {
		Method string `json:"method"`
		Info   string `json:"info"`
	}

	data := &JSONReply{
		Method: "info",
		Info:   info,
	}
	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	if err := defines.Conns.WriteMessage(websocket.TextMessage, msg); err != nil {
		log.Fatal(err)
	}
}

func sendshutdownmsg(info string) {
	type JSONReply struct {
		Method string `json:"method"`
		Info   string `json:"info"`
	}

	data := &JSONReply{
		Method: "shutdown",
		Info:   info,
	}
	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	if err := defines.Conns.WriteMessage(websocket.TextMessage, msg); err != nil {
		log.Fatal(err)
	}
}

func sendfatalmsg(info string) {
	type JSONReply struct {
		Method string `json:"method"`
		Info   string `json:"info"`
	}

	data := &JSONReply{
		Method: "fatal",
		Info:   info,
	}
	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	if err := defines.Conns.WriteMessage(websocket.TextMessage, msg); err != nil {
		log.Fatal(err)
	}
}
