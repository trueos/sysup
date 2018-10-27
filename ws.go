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

	"github.com/gorilla/websocket"
)

var (
	addr    = flag.String("addr", "127.0.0.1:8134", "Websocket service address")
)

var upgrader = websocket.Upgrader{} // use default options

var pkgflags string

func readws(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
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
					log.Println("Starting update check")
					checkforupdates()
				}

			default:
				log.Println("Uknown JSON KEY:", k)
		    }
		}

		log.Printf("server-recv: %s", message)
		err = c.WriteMessage(mt, message)
		if err != nil {
			log.Println("write:", err)
			break
		}
	}
}

func preparepkgconfig() {
	derr := os.MkdirAll("/var/db/upgrade-go/pkgdb", 0755)
	if derr != nil {
		log.Fatal(derr)
	}

	fdata := `PKG_CACHEDIR: /var/cache/upgrade-go
PKG_DBDIR: /var/db/upgrade-go/pkgdb
IGNORE_OSVERSION: YES`
	ioutil.WriteFile("/var/db/upgrade-go/pkg.conf", []byte(fdata), 0644)

}

func updatepkgdb() {
	cmd := exec.Command("pkg-static", "update", "-f")
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
	fmt.Println(allText)
	if err := cmd.Wait(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Done with pkg update")
}

func checkforupdates() {
	preparepkgconfig()
	updatepkgdb()

}
