package main

import (
	"encoding/json"
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
)

// Setup our CLI Flags
var benameflag string
var checkflag bool
var fullupdateflag bool
var stage2flag bool
var updateflag bool
var updatefileflag string
var updatekeyflag string
var websocketflag bool
func init() {
	flag.BoolVar(&checkflag, "check", false, "Check system status")
	flag.BoolVar(&updateflag, "update", false, "Update to latest packages")
	flag.BoolVar(&fullupdateflag, "fullupdate", false, "Force a full update")
	flag.BoolVar(&stage2flag, "stage2", false, "Start stage2 of an update (Normally used internally only)")
	flag.StringVar(&updatefileflag, "updatefile", "", "Use the specified update image instead of fetching from remote")
	flag.StringVar(&updatekeyflag, "updatekey", "", "Use the specified update pubkey for offline updates (Defaults to none)")
	flag.StringVar(&benameflag, "bename", "", "Set the name of the new boot-environment for updating. Must not exist yet.")
	flag.BoolVar(&websocketflag, "websocket", false, "Start websocket server for direct API access and events")
	flag.Parse()
}

type Envelope struct {
	Method string
}

type Check struct {
	Updates bool
	Details UpdateInfo
}

type UpdateReq struct {
	Method string `json:"method"`
	Bename string `json:"bename"`
	Fullupdate bool `json:"fullupdate"`
	Updatefile string `json:"updatefile"`
	Updatekey string `json:"updatekey"`
}

type InfoMsg struct {
	Info string
}

func rotatelog() {
	if _, err := os.Stat(logfile) ; os.IsNotExist(err) {
		return
	}
	cmd := exec.Command("mv", logfile, logfile + ".previous")
	cmd.Run()
}

func logtofile(info string) {
	f, err := os.OpenFile(logfile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := f.Write([]byte(info + "\n")); err != nil {
		log.Fatal(err)
	}
	if err:= f.Close(); err != nil {
		log.Fatal(err)
	}
}

func printupdatedetails(details UpdateInfo) {

	fmt.Println("The following packages will be updated:")
	fmt.Println("----------------------------------------------------")
	for i, _ := range details.Up {
		fmt.Println("   " + details.Up[i].Name + " " + details.Up[i].OldVersion + " -> " + details.Up[i].NewVersion)
	}

	fmt.Println()
	fmt.Println("The following packages will be installed:")
	fmt.Println("----------------------------------------------------")
	for i, _ := range details.New {
		fmt.Println("   " + details.New[i].Name + " " + details.New[i].Version)
	}

	fmt.Println()
	fmt.Println("The following packages will be removed:")
	fmt.Println("----------------------------------------------------")
	for i, _ := range details.Del {
		fmt.Println("   " + details.Del[i].Name + " " + details.Del[i].Version)
	}

	if ( details.KernelUp ) {
		fmt.Println()
		fmt.Println("Kernel Update will be performed - Two reboots required")
		fmt.Println("----------------------------------------------------")
	}
}

func parsejsonmsg(message []byte) int {
	if ( ! json.Valid(message) ) {
		log.Println("ERROR: Invalid JSON in return")
		return 1
	}
	//log.Printf("client-recv: %s", message)
	var env Envelope
	if err := json.Unmarshal(message, &env); err != nil {
		log.Fatal(err)
	}
	switch env.Method {
	case "check":
		var s struct {
			Envelope
			Check
		}
		if err := json.Unmarshal(message, &s); err != nil {
			log.Fatal(err)
		}
		var haveupdates bool = s.Updates
		if ( haveupdates ) {
			fmt.Println("The following updates are available")
			printupdatedetails(s.Details)
			os.Exit(10)
		} else {
			fmt.Println("No updates available")
			os.Exit(0)
		}
	case "info":
		var s struct {
			Envelope
			InfoMsg
		}
		if err := json.Unmarshal(message, &s); err != nil {
			log.Fatal(err)
		}
		var infomsg string = s.Info
		fmt.Println(infomsg)
	case "shutdown":
		var s struct {
			Envelope
			InfoMsg
		}
		if err := json.Unmarshal(message, &s); err != nil {
			log.Fatal(err)
		}
		var infomsg string = s.Info
		fmt.Println(infomsg)
		os.Exit(0)
	case "fatal":
		var s struct {
			Envelope
			InfoMsg
		}
		if err := json.Unmarshal(message, &s); err != nil {
			log.Fatal(err)
		}
		var infomsg string = s.Info
		fmt.Println(infomsg)
		os.Exit(150)

	default:
		log.Fatalf("unknown message type: %q", env.Method)
	}
	return 0
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
		// Do things with the message back
		parsejsonmsg(message)
	}
}


// Start the websocket server
func startws() {
        log.SetFlags(0)
        http.HandleFunc("/ws", readws)
        log.Fatal(http.ListenAndServe(*addr, nil))
}

// Start our client connection to the WS server
var (
        c   *websocket.Conn
)
func connectws() {
	log.SetFlags(0)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	u := url.URL{Scheme: "ws", Host: *addr, Path: "/ws"}
//	log.Printf("connecting to %s", u.String())

	err := errors.New("")
	var connected bool = false
	for attempt := 0; attempt < 10; attempt++ {
		c, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
		if err == nil {
			connected = true
			break
		}
		time.Sleep(5 * time.Millisecond);
	}
	if (!connected) {
		log.Fatal("Failed connecting to websocket server", err)
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

func startupdate() {
        data := &UpdateReq{
                Method:	"update",
                Fullupdate: fullupdateflag,
                Bename:   benameflag,
                Updatefile:   updatefileflag,
        }

	msg, err := json.Marshal(data)
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
		// Do things with the message back
		parsejsonmsg(message)
	}
}

func checkuid() {
	user, _ := user.Current()
	if ( user.Uid != "0" ) {
		fmt.Println("ERROR: Must be run as root")
		os.Exit(1)
	}
}

func main() {

	checkuid()

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
	if ( updateflag ) {
		go startws()
		connectws()
		startupdate()
		closews()
		os.Exit(0)
	}
	if ( stage2flag ) {
		startstage2()
		os.Exit(0)
	}
	if ( websocketflag ) {
		startws()
		os.Exit(0)
	}
}
