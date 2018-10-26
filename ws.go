package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var (
	addr    = flag.String("addr", "127.0.0.1:8134", "Websocket service address")
)

var upgrader = websocket.Upgrader{} // use default options

func echo(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		if ( ! json.Valid(message) ) {
			log.Println("INVALID JSON")
			continue

		}

		// Start decoding the incoming JSON
		var f interface{}
		err = json.Unmarshal(message, &f)
		m := f.(map[string]interface{})

		for k, v := range m {
		    switch k {
			case "method":
				if ( v == "check" ) {
					log.Println("Starting update check")
					checkforupdates()
				}

			default:
				log.Println("Uknown JSON KEY")
		    }
		}

		log.Printf("server-recv: %s", message)
		err = c.WriteMessage(mt, message)
		if err != nil {
			log.Println("write:", err)
			break
		}
	}
}

func checkforupdates() {

}
