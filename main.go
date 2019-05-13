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
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/trueos/sysup/client"
	"github.com/trueos/sysup/defines"
	"github.com/trueos/sysup/logger"
	"github.com/trueos/sysup/pkg"
	"github.com/trueos/sysup/trains"
	"github.com/trueos/sysup/update"
	"github.com/trueos/sysup/utils"
	"github.com/trueos/sysup/ws"
)

// Set up the websocket address
func setupWs() {
	if defines.WebsocketPort == 0 {
		port, err := utils.GetFreePort()

		if err != nil {
			log.Fatalln(err)
		}

		defines.WebsocketPort = port
	}

	// We couldn't get a free port
	if defines.WebsocketPort == 0 {
		log.Println("ERROR: No free port available to listen on!")
		os.Exit(100)
	}

	defines.WebsocketAddr = defines.WebsocketIP + ":" + strconv.Itoa(
		defines.WebsocketPort,
	)
}

// Start the websocket server
func startws(done chan bool) {
	log.SetFlags(0)
	http.HandleFunc("/ws", readws)

	if err := http.ListenAndServe(defines.WebsocketAddr, nil); err != nil {
		logger.LogToFile("ERROR: " + err.Error())
		log.Fatal(err)
	}

	done <- true
}

func connectws(done chan bool) {
	//Try (and fail as needed) to get the websocket started
	// This will instantly fail if a websocket server is already running there
	go startws(done)
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

		// We don't care about casing
		env.Method = strings.ToLower(env.Method)

		// A few ways to say die
		if env.Method == "quit" || env.Method == "exit" {
			env.Method = "shutdown"
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
		case "shutdown":
			ws.SendMsg("Shutting down sysup", "shutdown")
			os.Exit(0)
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
	done := make(chan bool)
	setupWs()

	if defines.BootloaderFlag {
		connectws(done)
		client.UpdateBootLoader()
		ws.CloseWs()
		<-done
		os.Exit(0)
	}

	if defines.ListTrainFlag {
		connectws(done)
		client.ListTrains()
		ws.CloseWs()
		<-done
		os.Exit(0)
	}

	if defines.ChangeTrainFlag != "" {
		connectws(done)
		client.SetTrain()
		ws.CloseWs()
		<-done
		os.Exit(0)
	}

	if defines.CheckFlag {
		connectws(done)
		client.StartCheck()
		ws.CloseWs()
		<-done
		os.Exit(0)
	}

	if defines.UpdateFlag || defines.FullUpdateFlag {
		connectws(done)
		client.StartUpdate()
		ws.CloseWs()
		<-done
		os.Exit(0)
	}

	if defines.WebsocketFlag {
		go startws(done)
		log.Println("Listening on", defines.WebsocketAddr)
		logger.LogToFile("Listening on " + defines.WebsocketAddr)
		<-done
		os.Exit(0)
	}
}
