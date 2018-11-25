package main

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"log"
	"os"
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

	// Now fetch the sig
	sendinfomsg("Fetching trains signature")
	sresp, serr := http.Get(trainsurl + ".sha1")
	if serr != nil {
		sendfatalmsg("ERROR: Failed fetching " + trainsurl + ".sha1")
	}

	// Cleanup when we exit
	defer sresp.Body.Close()

	// Load the file into memory
	sdat, err := ioutil.ReadAll(sresp.Body)
	if ( err != nil ) {
		sendfatalmsg("Failed reading train signature file!")
	}

	// Load the PEM key
	trainpub, terr := loadtrainspub()
	if ( terr != nil ) {
		log.Fatal("Failed to load train pubkey!")
	}
	block, _ := pem.Decode(trainpub)
	if block == nil || block.Type != "PUBLIC KEY" {
		log.Fatal("failed to decode PEM block containing public key")
	}

	// Get the public key from PEM
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		log.Fatal(err)
	}

	hashed := sha512.Sum512(dat)

	// Now verify the signatures match
	err = rsa.VerifyPKCS1v15(pub.(*rsa.PublicKey), crypto.SHA512, hashed[:], sdat)
	if err != nil {
		log.Fatal("Failed trains verification!")
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

func loadtrainspub() ([]byte, error) {
	var dat []byte
	// Try to load the default config file
        if _, err := os.Stat(trainpubkey) ; os.IsNotExist(err) {
                return dat, err
        }

        // Load the file into memory
	dat, err := ioutil.ReadFile(trainpubkey)
        if ( err != nil ) {
                log.Println("Failed reading train pubkey: " + trainpubkey )
                return dat, err
        }
	return dat, nil
}
