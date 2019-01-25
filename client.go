package main

import (
        "encoding/json"
        "fmt"
        "log"
        "os"

        "github.com/gorilla/websocket"
)


// Show us our list of trains
func printtrains(trains []TrainDef, deftrain string) {
	fmt.Println("Current Train: " + deftrain)
	fmt.Println("")
	fmt.Println("The following trains are available:")
	fmt.Println("------------------------------------------------------------------")
	for i, _ := range trains {
		fmt.Printf("%s\t\t\t%s", trains[i].Name, trains[i].Description)
		if ( trains[i].Deprecated ) {
			fmt.Printf(" [Deprecated]")
		}
		for j, _ := range trains[i].Tags {
			fmt.Printf(" [%s]", trains[i].Tags[j])
		}
		fmt.Printf("\n")
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
	case "updatebootloader":
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
	case "listtrains":
		var s struct {
			Envelope
			Trains []TrainDef `json:"trains"`
			Default string `json:"default"`
		}
		if err := json.Unmarshal(message, &s); err != nil {
			log.Fatal(err)
		}
		printtrains(s.Trains, s.Default)
		os.Exit(0)
	case "settrain":
		var s struct {
			Envelope
			Train string `json:"train"`
		}
		if err := json.Unmarshal(message, &s); err != nil {
			log.Fatal(err)
		}
		log.Println("Train set to: " + s.Train)
		os.Exit(0)
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
		log.Println("ERROR: " + infomsg)
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

func updatebootloader() {
        data := &SendReq{
                Method:	"updatebootloader",
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

func listtrains() {
        data := &SendReq{
                Method:	"listtrains",
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

func settrain() {
        data := &SendReq{
                Method:	"settrain",
                Train:	changetrainflag,
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

func startupdate() {
        data := &SendReq{
                Method:	"update",
                Fullupdate: fullupdateflag,
                Cachedir: cachedirflag,
                Bename:   benameflag,
                Disablebs:   disablebsflag,
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
