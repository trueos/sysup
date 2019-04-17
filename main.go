package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"time"

	"github.com/gorilla/websocket"
	"github.com/trueos/sysup/defines"
)

func rotatelog() {
	if _, err := os.Stat(defines.LogFile); os.IsNotExist(err) {
		return
	}
	cmd := exec.Command("mv", defines.LogFile, defines.LogFile+".previous")
	cmd.Run()
}

func logtofile(info string) {
	f, err := os.OpenFile(
		defines.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644,
	)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := f.Write([]byte(info + "\n")); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

// Start the websocket server
func startws() {
	log.SetFlags(0)
	http.HandleFunc("/ws", readws)
	log.Println("Listening on", defines.WebsocketAddr)

	//Make this non-fatal so it can be run every time (will fail *instantly* if a websocket is already running on that address)
	http.ListenAndServe(defines.WebsocketAddr, nil)

	//log.Fatal(http.ListenAndServe(*addr, nil))
}

// Start our client connection to the WS server
var (
	c *websocket.Conn
)

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
		//Note: This can take up to 45 seconds to timeout if the websocket server is not running
		c, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
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

// Called when we want to signal that its time to close the WS connection
func closews() {
	log.Println("Closing WS connection")
	log.Printf("closing ws")
	defer c.Close()

	// Cleanly close the connection by sending a close message and then
	// waiting (with timeout) for the server to close the connection.
	err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		log.Println("write close:", err)
		return
	}
	time.Sleep(10 * time.Millisecond)
}

func checkuid() {
	user, err := user.Current()
	if err != nil {
		fmt.Println(err)
		fmt.Println("Failed getting user.Current()")
		os.Exit(1)
		return
	}
	if user.Uid != "0" {
		fmt.Println("ERROR: Must be run as root")
		os.Exit(1)
	}
}

func setlocs() {
	// Check if the user provided their own location to store temp data
	if defines.CacheDirFlag == "" {
		return
	}

	defines.SysUpDb = defines.CacheDirFlag
	defines.PkgDb = defines.SysUpDb + "/pkgdb"
	defines.ImgMnt = defines.SysUpDb + "/mnt"
	defines.PkgConf = defines.SysUpDb + "/pkg.conf"
	defines.CacheDir = defines.SysUpDb + "/cache"

}

func main() {

	if len(os.Args) == 1 {
		flag.Usage()
		os.Exit(1)
	}

	// Update any variable locations
	setlocs()

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
		updatebootloader()
		closews()
		os.Exit(0)
	}

	if defines.ListTrainFlag {
		connectws()
		listtrains()
		closews()
		os.Exit(0)
	}

	if defines.ChangeTrainFlag != "" {
		connectws()
		settrain()
		closews()
		os.Exit(0)
	}

	if defines.CheckFlag {
		connectws()
		startcheck()
		closews()
		os.Exit(0)
	}

	if defines.UpdateFlag {
		connectws()
		startupdate()
		closews()
		os.Exit(0)
	}

	if defines.WebsocketFlag {
		startws()
		os.Exit(0)
	}
}
