package main

import (
	"bufio"
	"crypto"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/gorilla/websocket"
)

// Load the trains from remote
func loadtrains() (TrainsDef, error) {

	// Create our JSON struct
	s := TrainsDef{ }

	if ( trainsurl == "" ) {
		sendfatalmsg("No train URL defined in JSON configuration: " + configjson )
		return s, errors.New("ERROR")
	}

	//sendinfomsg("Fetching trains configuration")
	resp, err := http.Get(trainsurl)
	if err != nil {
		sendfatalmsg("ERROR: Failed fetching " + trainsurl)
		return s, errors.New("ERROR")
	}

	// Cleanup when we exit
	defer resp.Body.Close()

	// Load the file into memory
	dat, err := ioutil.ReadAll(resp.Body)
	if ( err != nil ) {
		sendfatalmsg("Failed reading train file!")
		return s, errors.New("ERROR")
	}

	// Now fetch the sig
	//sendinfomsg("Fetching trains signature")
	sresp, serr := http.Get(trainsurl + ".sha1")
	if serr != nil {
		sendfatalmsg("ERROR: Failed fetching " + trainsurl + ".sha1")
		return s, errors.New("ERROR")
	}

	// Cleanup when we exit
	defer sresp.Body.Close()

	// Load the file into memory
	sdat, err := ioutil.ReadAll(sresp.Body)
	if ( err != nil ) {
		sendfatalmsg("Failed reading train signature file!")
		return s, errors.New("ERROR")
	}

	// Load the PEM key
	trainpub, terr := loadtrainspub()
	if ( terr != nil ) {
		sendfatalmsg("Failed to load train pubkey!")
		return s, errors.New("ERROR")
	}
	block, _ := pem.Decode(trainpub)
	if block == nil || block.Type != "PUBLIC KEY" {
		sendfatalmsg("failed to decode PEM block containing public key")
		return s, errors.New("ERROR")
	}

	// Get the public key from PEM
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		sendfatalmsg("Failed to parse pub key")
		return s, errors.New("ERROR")
	}

	hashed := sha512.Sum512(dat)

	// Now verify the signatures match
	err = rsa.VerifyPKCS1v15(pub.(*rsa.PublicKey), crypto.SHA512, hashed[:], sdat)
	if err != nil {
		sendfatalmsg("Failed trains verification!")
		return s, errors.New("ERROR")
	}

	// Lets decode this puppy
	if err := json.Unmarshal(dat, &s); err != nil {
		sendfatalmsg("Failed JSON parsing of train file!")
		return s, errors.New("ERROR")
	}

	// Get the default train
	deftrain, terr := getdefaulttrain()
	if ( terr == nil ) {
		s.Default = deftrain
	}

	return s, nil
}

// Get trains and reply
func dotrainlist() {
	trains, err := loadtrains()
	if ( err != nil ) { return }
	sendtraindetails(trains)
}

// Send back details about the train
func sendtraindetails(trains TrainsDef) {
        type JSONReply struct {
                Method string `json:"method"`
                Trains  []TrainDef `json:"trains"`
		Default string `json:"default"`
        }

        data := &JSONReply{
                Method:     "listtrains",
                Trains:   trains.Trains,
                Default:   trains.Default,
        }
        msg, err := json.Marshal(data)
        if err != nil {
                log.Fatal("Failed encoding JSON:", err)
        }
        if err := conns.WriteMessage(websocket.TextMessage, msg); err != nil {
                log.Fatal(err)
        }
}

func getdefaulttrain() (string, error) {
	var deftrain string
	fileHandle, err := os.Open("/etc/pkg/Train.conf")
	if ( err != nil ) {
		return deftrain, err
	}
	defer fileHandle.Close()
	fileScanner := bufio.NewScanner(fileHandle)

	for fileScanner.Scan() {
		line := fileScanner.Text()
                if ( strings.Contains(line, "# TRAINNAME ")) {
			linearray := strings.Split(line, " ")
			if ( len(linearray) < 2) {
				continue
			}
			deftrain = strings.TrimSpace(linearray[2])
			break
		}
	}

	return deftrain, nil
}

// Load the default trains pub key we use to verify JSON validity
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

func createnewpkgconf( train TrainDef ) {
	// Nuke existing pkg configs
	cmd := exec.Command("/bin/sh", "-c", "rm -f /etc/pkg/*.conf")
        cmd.Run()

	// Write the new key file
	var kdata []string
	for i, _ := range train.PkgKey {
		kdata = append(kdata, train.PkgKey[i])
	}
        ioutil.WriteFile("/usr/share/keys/train-pkg.key", []byte(strings.Join(kdata, "\n")), 0644)


	// Write the new conf file
	fdata := `# TRAINNAME ` + train.Name + `

Train: {
  url: "` + train.PkgURL + `",
  signature_type: "pubkey",
  pubkey: "/usr/share/keys/train-pkg.key",
  enabled: yes
}
`
        ioutil.WriteFile("/etc/pkg/Train.conf", []byte(fdata), 0644)

}

func dosettrain(message []byte) {
	// Get the new train name from JSON
	var s struct {
		Envelope
		SendReq
	}
	if err := json.Unmarshal(message, &s); err != nil {
		log.Fatal(err)
	}
	var newtrain = s.Train

	// Load the current train list
	trainlist, err := loadtrains()
	if ( err != nil ) { return }

	var foundt = -1
	trains := trainlist.Trains
	for i, _ := range trains {
		if ( trains[i].Name == newtrain ) {
			foundt = i
			break
		}
	}
	if ( foundt == -1 ) {
		sendfatalmsg("Invalid train specified: " + newtrain)
		return
	}

	// Sanity check
	if ( trains[foundt].PkgURL == "" ) {
		sendfatalmsg("Train missing PkgURL")
		return
	}

	// Set the new train config file
	createnewpkgconf(trains[foundt])

	// Send back confirmation
        type JSONReply struct {
                Method string `json:"method"`
                Train  string `json:"train"`
        }

        data := &JSONReply{
                Method:     "settrain",
                Train:   newtrain,
        }
        msg, err := json.Marshal(data)
        if err != nil {
                log.Fatal("Failed encoding JSON:", err)
        }
        if err := conns.WriteMessage(websocket.TextMessage, msg); err != nil {
                log.Fatal(err)
        }

}
