package main

import (
	"encoding/json"
	"log"
	"io/ioutil"
	"net/http"

	"github.com/gorilla/websocket"
)

func loadtrains() TrainsDef {

	if ( trainsurl == "" ) {
		sendfatalmsg("No train URL defined in JSON configuration: " + configjson )
	}

	sendinfomsg("Fetching trains configuration")
	resp, err := http.Get(trainsurl)
	if err != nil {
		sendfatalmsg("ERROR: Failed fetching " + trainsurl)
	}

	// Cleanup when we exit
	defer resp.Body.Close()

	// Load the file into memory
	dat, err := ioutil.ReadAll(resp.Body)
	if ( err != nil ) {
		sendfatalmsg("Failed reading train file!")
	}

	// Create our JSON struct
	s := TrainsDef{ }

	// Lets decode this puppy
	if err := json.Unmarshal(dat, &s); err != nil {
		log.Println(err)
		sendfatalmsg("Failed JSON parsing of train file!")
	}
	return s
}

func dotrainlist() {
	trains := loadtrains()
	sendtraindetails(trains)
}

func sendtraindetails(trains TrainsDef) {
        type JSONReply struct {
                Method string `json:"method"`
                Trains  []TrainDef `json:"trains"`
        }

        data := &JSONReply{
                Method:     "listtrains",
                Trains:   trains.Trains,
        }
        msg, err := json.Marshal(data)
        if err != nil {
                log.Fatal("Failed encoding JSON:", err)
        }
        if err := conns.WriteMessage(websocket.TextMessage, msg); err != nil {
                log.Fatal(err)
        }
}
