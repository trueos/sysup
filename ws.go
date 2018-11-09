package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/gorilla/websocket"
)

var (
	addr    = flag.String("addr", "127.0.0.1:8134", "Websocket service address")
)

var updater = websocket.Upgrader{} // use default options
// Start our client connection to the WS server
var (
        conns   *websocket.Conn
)
var pkgflags string

func readws(w http.ResponseWriter, r *http.Request) {
	var err error
	conns, err = updater.Upgrade(w, r, nil)
	if err != nil {
		log.Print("update:", err)
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
	        var env Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			log.Fatal(err)
	        }
	        switch env.Method {
	        case "check":
			checkforupdates()
		case "update":
	                var s struct {
				Envelope
				UpdateReq
			}
			if err = json.Unmarshal(message, &s); err != nil {
				log.Fatal(err)
			}
			doupdate(s.Updatefile)
		default:
			log.Println("Uknown JSON Method:", env.Method)
		}

		// log.Printf("server-recv: %s", message)
		//err = conns.WriteMessage(mt, message)
		//if err != nil {
		//	log.Println("write:", err)
		//	break
		//}
	}
}

func doupdate(updatefile string) {
	log.Println("updatefile: " + updatefile)

	// Setup the pkg config directory
	preparepkgconfig()

	// Update the package database
	updatepkgdb()

	// Check that updates are available
	_, haveupdates := updatedryrun(false)
	if ( ! haveupdates ) {
		sendfatalmsg("ERROR: No updates to install!")
		return
	}

	// Check host OS version
	checkosver()

	sendinfomsg("Starting package downloads")
	//startfetch()
	os.Exit(0)
}

func checkosver() {
	// Check the host OS version
	OSINT, oerr := syscall.SysctlUint32("kern.osreldate")
	if ( oerr != nil ) {
		log.Fatal(oerr)
	}
	REMOTEVER, err := getremoteosver()
	if ( err != nil ) {
		log.Fatal(err)
	}

	OSVER := fmt.Sprint(OSINT)
	if ( OSVER != REMOTEVER ) {
		sendinfomsg("Remote ABI change detected. Doing full update.")
	}
}

func getremoteosver() (string, error) {

	cmd := exec.Command("pkg-static", "-C", localpkgconf, "rquery", "%At=%Av", "ports-mgmt/pkg")
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

	scanner := bufio.NewScanner(strings.NewReader(strings.Join(allText, "\n")))
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// Skip empty lines
		if len(line) == 0 {
			continue
		}
		if ( strings.Contains(line, "FreeBSD_version=")) {
			strarray := strings.Split(line, "=")
			return string(strarray[1]), nil
		}
	}
	return "", fmt.Errorf("Failed to get FreeBSD_version", allText)
}

var localpkgdb = "/var/db/update-go/pkgdb"
var localpkgconf = "/var/db/update-go/pkg.conf"
var localcachedir = "/var/cache/update-go"
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

func sendfatalmsg(info string) {
	type JSONReply struct {
		Method string `json:"method"`
		Info  string `json:"info"`
	}

	data := &JSONReply{
		Method:     "fatal",
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

func updatedryrun(sendupdate bool) (*UpdateInfo, bool) {
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

	haveupdates := ! strings.Contains(strings.Join((allText), "\n"), "Your packages are up to date")
	details := UpdateInfo{ }
	updetails := &details
	if ( haveupdates ) {
		updetails = parseupdatedata(allText)
	}
	if ( sendupdate ) {
		sendupdatedetails(haveupdates, updetails)
	}
	return updetails, haveupdates
}

func sendupdatedetails(haveupdates bool, updetails *UpdateInfo) {
	type JSONReply struct {
		Method string `json:"method"`
		Updates  bool `json:"updates"`
		Details  *UpdateInfo `json:"details"`
	}

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
}

func checkforupdates() {
	preparepkgconfig()
	updatepkgdb()
	updatedryrun(true)
}
