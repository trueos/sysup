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
	"os/user"
	"time"

	"github.com/gorilla/websocket"
	"github.com/trueos/sysup/client"
	"github.com/trueos/sysup/defines"
	"github.com/trueos/sysup/pkg"
	"github.com/trueos/sysup/trains"
	"github.com/trueos/sysup/update"
	"github.com/trueos/sysup/ws"
)

// Start the websocket server
func startws() {
	log.SetFlags(0)
	http.HandleFunc("/ws", readws)

	// This isn't applicable when they aren't invoking sysup as a server
	if defines.WebsocketFlag {
		log.Println("Listening on", defines.WebsocketAddr)
	}

	//Make this non-fatal so it can be run every time (will fail *instantly*
	//if a websocket is already running on that address)
	http.ListenAndServe(defines.WebsocketAddr, nil)

	//log.Fatal(http.ListenAndServe(*addr, nil))
}

func connectws() {
	//Try (and fail as needed) to get the websocket started
	// This will instantly fail if a websocket server is already running there
	go startws()
	log.SetFlags(0)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	u := url.URL{Scheme: "ws", Host: defines.WebsocketAddr, Path: "/ws"}
	//log.Printf("connecting to %s", u.String())

	err := errors.New("")
	var connected bool = false
	for attempt := 0; attempt < 5; attempt++ {
		//Note: This can take up to 45 seconds to timeout if the websocket
		//server is not running
		defines.WSClient, _, err = websocket.DefaultDialer.Dial(
			u.String(), nil,
		)
		if err == nil {
			connected = true
			break
		}
		//log.Printf("Failed connection: %s", attempt)
		time.Sleep(100 * time.Millisecond)
	}
	if !connected {
		log.Fatal("Failed connecting to websocket server", err)
	}
}

// Parses the message and calls the responsible module
func readws(w http.ResponseWriter, r *http.Request) {
	var err error
	defines.WSServer, err = defines.Updater.Upgrade(w, r, nil)
	if err != nil {
		log.Print("update:", err)
		return
	}
	defer defines.WSServer.Close()
	for {
		_, message, err := defines.WSServer.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}

		if !json.Valid(message) {
			log.Println("INVALID JSON")
			ws.SendMsg("INVALID JSON", "fatal")
			continue
		}

		// Start decoding the incoming JSON
		var env defines.Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			ws.SendMsg("Invalid JSON received")
			log.Println("Warning: Invalid JSON message received")
			log.Println(err)
		}
		switch env.Method {
		case "check":
			pkg.CheckForUpdates()
		case "listtrains":
			trains.DoTrainList()
		case "settrain":
			trains.DoSetTrain(message)
		case "update":
			update.DoUpdate(message)
		case "updatebootloader":
			update.UpdateLoader("")
			ws.SendMsg("Finished bootloader process", "updatebootloader")
		default:
			log.Println("Uknown JSON Method:", env.Method)
		}

		// log.Printf("server-recv: %s", message)
		//err = defines.WSServers.WriteMessage(mt, message)
		//if err != nil {
		//	log.Println("write:", err)
		//	break
		//}
	}
}

func checkuid() {
	user, err := user.Current()
	if err != nil {
		log.Println(err)
		log.Println("Failed getting user.Current()")
		os.Exit(1)
		return
	}
	if user.Uid != "0" {
		log.Println("ERROR: Must be run as root")
		os.Exit(1)
	}
}

func main() {

	if len(os.Args) == 1 {
		flag.Usage()
		os.Exit(1)
	}

	// Update any variable locations
	defines.SetLocs()

	// Capture any sigint
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		<-interrupt
		os.Exit(1)
	}()

	// Load the local config file if it exists
	defines.LoadConfig()

	if defines.BootloaderFlag {
		connectws()
		client.UpdateBootLoader()
		ws.CloseWs()
		os.Exit(0)
	}

	if defines.ListTrainFlag {
		connectws()
		client.ListTrains()
		ws.CloseWs()
		os.Exit(0)
	}

	if defines.ChangeTrainFlag != "" {
		connectws()
		client.SetTrain()
		ws.CloseWs()
		os.Exit(0)
	}

	if defines.CheckFlag {
		connectws()
		client.StartCheck()
		ws.CloseWs()
		os.Exit(0)
	}

	if defines.UpdateFlag || defines.FullUpdateFlag {
		connectws()
		client.StartUpdate()
		ws.CloseWs()
		os.Exit(0)
	}

	if defines.WebsocketFlag {
		startws()
		os.Exit(0)
	}
}
