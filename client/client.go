package client

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/trueos/sysup/defines"
	"log"
	"os"
)

// Show us our list of trains
func printtrains(trains []defines.TrainDef, deftrain string) {
	fmt.Println("Current Train: " + deftrain)
	fmt.Println("")
	fmt.Println("The following trains are available:")
	fmt.Println(
		"------------------------------------------------------------------",
	)
	for i := range trains {
		fmt.Printf("%s\t\t\t%s", trains[i].Name, trains[i].Description)
		if trains[i].Deprecated {
			fmt.Printf(" [Deprecated]")
		}
		for j := range trains[i].Tags {
			fmt.Printf(" [%s]", trains[i].Tags[j])
		}
		fmt.Printf("\n")
	}
}

func parsejsonmsg(message []byte) int {
	if !json.Valid(message) {
		log.Println("ERROR: Invalid JSON in return")
		return 1
	}
	//log.Printf("client-recv: %s", message)
	var env defines.Envelope
	if err := json.Unmarshal(message, &env); err != nil {
		log.Fatal(err)
	}
	switch env.Method {
	case "check":
		var s struct {
			defines.Envelope
			defines.Check
		}
		if err := json.Unmarshal(message, &s); err != nil {
			log.Fatal(err)
		}
		var haveupdates bool = s.Updates
		if haveupdates {
			log.Println("The following updates are available")
			printupdatedetails(s.Details)
			os.Exit(10)
		} else {
			log.Println("No updates available")
			os.Exit(0)
		}
	case "info":
		var s struct {
			defines.Envelope
			defines.InfoMsg
		}
		if err := json.Unmarshal(message, &s); err != nil {
			log.Fatal(err)
		}
		var infomsg string = s.Info
		log.Println(infomsg)
	case "updatebootloader":
		var s struct {
			defines.Envelope
			defines.InfoMsg
		}
		if err := json.Unmarshal(message, &s); err != nil {
			log.Fatal(err)
		}
		var infomsg string = s.Info
		log.Println(infomsg)
		os.Exit(0)
	case "listtrains":
		var s struct {
			defines.Envelope
			Trains  []defines.TrainDef `json:"trains"`
			Default string             `json:"default"`
		}
		if err := json.Unmarshal(message, &s); err != nil {
			log.Fatal(err)
		}
		printtrains(s.Trains, s.Default)
		os.Exit(0)
	case "settrain":
		var s struct {
			defines.Envelope
			Train string `json:"train"`
		}
		if err := json.Unmarshal(message, &s); err != nil {
			log.Fatal(err)
		}
		log.Println("Train set to: " + s.Train)
		os.Exit(0)
	case "shutdown":
		var s struct {
			defines.Envelope
			defines.InfoMsg
		}
		if err := json.Unmarshal(message, &s); err != nil {
			log.Fatal(err)
		}
		var infomsg string = s.Info
		log.Println(infomsg)
		os.Exit(0)
	case "fatal":
		var s struct {
			defines.Envelope
			defines.InfoMsg
		}
		if err := json.Unmarshal(message, &s); err != nil {
			log.Fatal(err)
		}
		var infomsg string = s.Info
		log.Println("ERROR: " + infomsg)
		os.Exit(150)

	default:
		log.Fatalf("unknown message type: %q", env.Method)
	}
	return 0
}

func StartCheck() {
	data := map[string]string{
		"method": "check",
	}
	msg, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	//log.Println("JSON Message: ", string(msg))
	send_err := defines.WSClient.WriteMessage(websocket.TextMessage, msg)
	if send_err != nil {
		log.Fatal("Failed talking to WS backend:", send_err)
	}

	done := make(chan struct{})
	defer close(done)

	// Wait for messages back
	for {
		_, message, err := defines.WSClient.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return
		}
		// Do things with the message back
		parsejsonmsg(message)
	}
}

func UpdateBootLoader() {
	data := &defines.SendReq{
		Method: "updatebootloader",
	}

	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	//log.Println("JSON Message: ", string(msg))
	send_err := defines.WSClient.WriteMessage(websocket.TextMessage, msg)
	if send_err != nil {
		log.Fatal("Failed talking to WS backend:", send_err)
	}

	done := make(chan struct{})
	defer close(done)

	// Wait for messages back
	for {
		_, message, err := defines.WSClient.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return
		}
		// Do things with the message back
		parsejsonmsg(message)
	}
}

func ListTrains() {
	data := &defines.SendReq{
		Method: "listtrains",
	}

	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	//log.Println("JSON Message: ", string(msg))
	send_err := defines.WSClient.WriteMessage(websocket.TextMessage, msg)
	if send_err != nil {
		log.Fatal("Failed talking to WS backend:", send_err)
	}

	done := make(chan struct{})
	defer close(done)

	// Wait for messages back
	for {
		_, message, err := defines.WSClient.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return
		}
		// Do things with the message back
		parsejsonmsg(message)
	}
}

func SetTrain() {
	data := &defines.SendReq{
		Method: "settrain",
		Train:  defines.ChangeTrainFlag,
	}

	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	//log.Println("JSON Message: ", string(msg))
	send_err := defines.WSClient.WriteMessage(websocket.TextMessage, msg)
	if send_err != nil {
		log.Fatal("Failed talking to WS backend:", send_err)
	}

	done := make(chan struct{})
	defer close(done)

	// Wait for messages back
	for {
		_, message, err := defines.WSClient.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return
		}
		// Do things with the message back
		parsejsonmsg(message)
	}
}

func printupdatedetails(details defines.UpdateInfo) {

	log.Println("The following packages will be updated:")
	log.Println("----------------------------------------------------")
	for i := range details.Up {
		log.Println(
			"   " + details.Up[i].Name + " " + details.Up[i].OldVersion +
				" -> " + details.Up[i].NewVersion,
		)
	}

	log.Println()
	log.Println("The following packages will be installed:")
	log.Println("----------------------------------------------------")
	for i := range details.New {
		log.Println("   " + details.New[i].Name + " " + details.New[i].Version)
	}

	log.Println()
	log.Println("The following packages will be reinstalled:")
	log.Println("----------------------------------------------------")
	for i := range details.Ri {
		log.Println("   " + details.Ri[i].Name + " " + details.Ri[i].Reason)
	}

	log.Println()
	log.Println("The following packages will be removed:")
	log.Println("----------------------------------------------------")
	for i := range details.Del {
		log.Println("   " + details.Del[i].Name + " " + details.Del[i].Version)
	}
}

func StartUpdate() {
	data := &defines.SendReq{
		Method:     "update",
		Fullupdate: defines.FullUpdateFlag,
		Cachedir:   defines.CacheDirFlag,
		Bename:     defines.BeNameFlag,
		Disablebs:  defines.DisableBsFlag,
		Updatefile: defines.UpdateFileFlag,
	}

	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	//log.Println("JSON Message: ", string(msg))
	send_err := defines.WSClient.WriteMessage(websocket.TextMessage, msg)
	if send_err != nil {
		log.Fatal("Failed talking to WS backend:", send_err)
	}

	done := make(chan struct{})
	defer close(done)

	// Wait for messages back
	for {
		_, message, err := defines.WSClient.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return
		}
		// Do things with the message back
		parsejsonmsg(message)
	}
}
