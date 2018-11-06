package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"log"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/gorilla/websocket"
)

var (
	addr    = flag.String("addr", "127.0.0.1:8134", "Websocket service address")
)

var upgrader = websocket.Upgrader{} // use default options
// Start our client connection to the WS server
var (
        conns   *websocket.Conn
)
var pkgflags string

func readws(w http.ResponseWriter, r *http.Request) {
	var err error
	conns, err = upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
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
		var f interface{}
		err = json.Unmarshal(message, &f)
		m := f.(map[string]interface{})

		for k, v := range m {
		    switch k {
			case "method":
				if ( v == "check" ) {
					checkforupdates()
				}

			default:
				log.Println("Uknown JSON KEY:", k)
		    }
		}

		// log.Printf("server-recv: %s", message)
		//err = conns.WriteMessage(mt, message)
		//if err != nil {
		//	log.Println("write:", err)
		//	break
		//}
	}
}

var localpkgdb = "/var/db/upgrade-go/pkgdb"
var localpkgconf = "/var/db/upgrade-go/pkg.conf"
var localcachedir = "/var/cache/upgrade-go"
func preparepkgconfig() {
	derr := os.MkdirAll(localpkgdb, 0755)
	if derr != nil {
		log.Fatal(derr)
	}
	cerr := os.MkdirAll(localcachedir, 0755)
	if cerr != nil {
		log.Fatal(cerr)
	}

	fdata := `PKG_CACHEDIR: ` + localcachedir + `
PKG_DBDIR: ` + localpkgdb + `
IGNORE_OSVERSION: YES`
	ioutil.WriteFile(localpkgconf, []byte(fdata), 0644)

}

func updatepkgdb() {
	cmd := exec.Command("pkg-static", "-C", localpkgconf, "update", "-f")
	sendinfomsg("Updating package remote database")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	buff := bufio.NewScanner(stdout)
	// Iterate over buff and append content to the slice
	var allText []string
	for buff.Scan() {
		allText = append(allText, buff.Text()+"\n")
	}
	//fmt.Println(allText)
	if err := cmd.Wait(); err != nil {
		log.Fatal(err)
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

func upgradedryrun() {
	cmd := exec.Command("pkg-static", "-C", localpkgconf, "upgrade", "-Un")
	sendinfomsg("Checking system for updates")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and append content to the slice
	var allText []string
	for buff.Scan() {
		allText = append(allText, buff.Text()+"\n")
	}
	//fmt.Println(allText)
	if err := cmd.Wait(); err != nil {
		log.Fatal(err)
	}
	type JSONReply struct {
		Method string `json:"method"`
		Updates  bool `json:"updates"`
	}

	haveupdates := ! strings.Contains(strings.Join((allText), "\n"), "Your packages are up to date")
	data := &JSONReply{
		Method:     "check",
		Updates:   haveupdates,
	}
	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	if err := conns.WriteMessage(websocket.TextMessage, msg); err != nil {
		log.Fatal(err)
	}
}

func checkforupdates() {
	preparepkgconfig()
	updatepkgdb()
	upgradedryrun()
}
