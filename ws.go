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

	// Copy over the existing local database
	srcFolder := "/var/db/pkg/local.sqlite"
	destFolder := localpkgdb + "/local.sqlite"
	cpCmd := exec.Command("cp", "-f", srcFolder, destFolder)
	err := cpCmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
	}

	// Create the config file
	fdata := `PKG_CACHEDIR: ` + localcachedir + `
PKG_DBDIR: ` + localpkgdb + `
IGNORE_OSVERSION: YES`
	ioutil.WriteFile(localpkgconf, []byte(fdata), 0644)

}

func updatepkgdb() {
	cmd := exec.Command("pkg-static", "-d", "-C", localpkgconf, "update", "-f")
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

// Define all our JSON structures we use to return update info
//----------------------------------------------------
type NewPkg struct {
	Name string `json:"name"`
	Version string `json:"Version"`
}

type UpPkg struct {
	Name string `json:"name"`
	OldVersion string `json:"OldVersion"`
	NewVersion string `json:"NewVersion"`
}

type DelPkg struct {
	Name string `json:"name"`
	Version string `json:"Version"`
}

type UpdateInfo struct {
	New []NewPkg `json:"new"`
	Up []UpPkg `json:"update"`
	Del []DelPkg `json:"delete"`
}
//----------------------------------------------------

func parseupdatedata(uptext []string) *UpdateInfo {
	var stage string
	var line string

	// Init the structure
	details := UpdateInfo{ }
	detailsNew := NewPkg{ }
	detailsUp := UpPkg{ }
	detailsDel := DelPkg{ }

	scanner := bufio.NewScanner(strings.NewReader(strings.Join(uptext, "\n")))
	for scanner.Scan() {
		line = scanner.Text()
		line = strings.TrimSpace(line)

		// Skip empty lines
		if len(line) == 0 {
			continue
		}
		if ( strings.Contains(line, "INSTALLED:")) {
			stage = "NEW"
			continue
		}
		if ( strings.Contains(line, "UPGRADED:")) {
			stage = "UPGRADE"
			continue
		}
		if ( strings.Contains(line, "REMOVED:")) {
			stage = "REMOVE"
			continue
		}
		if ( strings.Contains(line, " to be installed:")) {
			stage = ""
			continue
		}
		if ( strings.Contains(line, " to be upgraded:")) {
			stage = ""
			continue
		}
//		fmt.Printf(line + "\n")
//		fmt.Printf("Fields are: %q\n", strings.Fields(line))
		switch stage {
		case "NEW":
			if ( strings.Contains(line, ": ")) {
				linearray := strings.Split(line, " ")
				if ( len(linearray) < 2) {
					continue
				}
				detailsNew.Name=linearray[0]
				detailsNew.Version=linearray[1]
				details.New = append(details.New, detailsNew)
				continue
			}
		case "UPGRADE":
			if ( strings.Contains(line, " -> ")) {
				linearray := strings.Split(line, " ")
				if ( len(linearray) < 4) {
					continue
				}
				detailsUp.Name=strings.Replace(linearray[0], ":", "", -1)
				detailsUp.OldVersion=linearray[1]
				detailsUp.NewVersion=linearray[3]
				details.Up = append(details.Up, detailsUp)
				continue
			}
		case "REMOVE":
			if ( strings.Contains(line, ": ")) {
				linearray := strings.Split(line, " ")
				if ( len(linearray) < 2) {
					continue
				}
				detailsDel.Name=linearray[0]
				detailsDel.Version=linearray[1]
				details.Del = append(details.Del, detailsDel)
				continue
			}
		default:
		}
	}
	//log.Print("UpdateInfo", details)
	return &details
}

func upgradedryrun() {
	cmd := exec.Command("pkg-static", "-C", localpkgconf, "upgrade", "-n")
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
	// Pkg returns 0 on sucess and 1 on updates needed
	//if err := cmd.Wait(); err != nil {
	//	log.Fatal(err)
	//}
	type JSONReply struct {
		Method string `json:"method"`
		Updates  bool `json:"updates"`
		Details  *UpdateInfo `json:"details"`
	}

	haveupdates := ! strings.Contains(strings.Join((allText), "\n"), "Your packages are up to date")
	if ( haveupdates ) {
		updetails := parseupdatedata(allText)
		data := &JSONReply{
			Method:     "check",
			Updates:   haveupdates,
			Details:   updetails,
		}
		msg, err := json.Marshal(data)
		if err != nil {
			log.Fatal("Failed encoding JSON:", err)
		}
		if err := conns.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Fatal(err)
		}
	} else {
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
}

func checkforupdates() {
	preparepkgconfig()
	updatepkgdb()
	upgradedryrun()
}
