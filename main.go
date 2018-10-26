package main

import (
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

// Setup our CLI Flags
var checkflag bool
var startflag bool
var websocketflag bool
func init() {
	flag.BoolVar(&checkflag, "check", false, "Check system status")
	flag.BoolVar(&startflag, "start", false, "Start and upgrade to latest packages")
	flag.BoolVar(&websocketflag, "websocket", false, "Start websocket server for direct API access and events")
	flag.Parse()
}

func startcheck() {
	data := map[string]string{
		"method": "check",
	}
	msg, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	//fmt.Println("JSON Message: ", string(msg))
	send_err := c.WriteMessage(websocket.TextMessage, msg)
	if send_err != nil {
		log.Fatal("Failed talking to WS backend:", send_err)
	}

	done := make(chan struct{})
	defer close(done)


	// Wait for messages back
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return
		}
		if ( ! json.Valid(message) ) {
			log.Println("ERROR: Invalid JSON in return")
			break
		}
		log.Printf("client-recv: %s", message)
	}
}


// Start the websocket server
func startws() {
        log.SetFlags(0)
        http.HandleFunc("/ws", echo)
        log.Fatal(http.ListenAndServe(*addr, nil))
}

// Start our client connection to the WS server
var (
        c   *websocket.Conn
)
func connectws() {
	time.Sleep(2 * time.Second);
	log.SetFlags(0)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	u := url.URL{Scheme: "ws", Host: *addr, Path: "/ws"}
	log.Printf("connecting to %s", u.String())

	err := errors.New("")
	c, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		time.Sleep(200 * time.Millisecond);
		c, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			log.Fatal("dial:", err)
		}
	}
}

// Called when we want to signal that its time to close the WS connection
func closews() {
	log.Println("Closing WS connection")
	defer c.Close()

	// Cleanly close the connection by sending a close message and then
	// waiting (with timeout) for the server to close the connection.
	err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		log.Println("write close:", err)
		return
	}
}

func main() {
	if len(os.Args) == 1 {
		flag.Usage()
	}

	// Capture any sigint
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		<-interrupt
		os.Exit(1)
	}()

	if ( checkflag ) {
		go startws()
		connectws()
		startcheck()
		closews()
		os.Exit(0)
	}
	if ( websocketflag ) {
		startws()
	}
}
