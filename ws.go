package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

func readws(w http.ResponseWriter, r *http.Request) {
	var err error
	conns, err = updater.Upgrade(w, r, nil)
	if err != nil {
		log.Print("update:", err)
		return
	}
	defer conns.Close()
	for {
		_, message, err := conns.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		if ( ! json.Valid(message) ) {
			log.Println("INVALID JSON")
			continue

		}

		// Start decoding the incoming JSON
	        var env Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			log.Fatal(err)
	        }
	        switch env.Method {
	        case "check":
			checkforupdates()
		case "update":
	                var s struct {
				Envelope
				UpdateReq
			}
			if err = json.Unmarshal(message, &s); err != nil {
				log.Fatal(err)
			}
			doupdate(s.Updatefile)
		default:
			log.Println("Uknown JSON Method:", env.Method)
		}

		// log.Printf("server-recv: %s", message)
		//err = conns.WriteMessage(mt, message)
		//if err != nil {
		//	log.Println("write:", err)
		//	break
		//}
	}
}

func sendinfomsg(info string) {
	type JSONReply struct {
		Method string `json:"method"`
		Info  string `json:"info"`
	}

	data := &JSONReply{
		Method:     "info",
		Info:   info,
	}
	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	if err := conns.WriteMessage(websocket.TextMessage, msg); err != nil {
		log.Fatal(err)
	}
}

func sendfatalmsg(info string) {
	type JSONReply struct {
		Method string `json:"method"`
		Info  string `json:"info"`
	}

	data := &JSONReply{
		Method:     "fatal",
		Info:   info,
	}
	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	if err := conns.WriteMessage(websocket.TextMessage, msg); err != nil {
		log.Fatal(err)
	}
}
