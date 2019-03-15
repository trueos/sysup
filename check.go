package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"log"
	"os/exec"
	"strings"

	"github.com/gorilla/websocket"
)

func parseupdatedata(uptext []string) *UpdateInfo {
	var stage string
	var line string

	// Init the structure
	details := UpdateInfo{ }
	detailsNew := NewPkg{ }
	detailsUp := UpPkg{ }
	detailsRi := RiPkg{ }
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
		if ( strings.Contains(line, "REINSTALLED:")) {
			stage = "REINSTALLED"
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
		if ( strings.Contains(line, " to be REINSTALLED:")) {
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
		case "REINSTALLED":
			if ( strings.Contains(line, " (")) {
				linearray := strings.Split(line, " (")
				if ( len(linearray) < 2) {
					continue
				}
				detailsRi.Name=linearray[0]
				detailsRi.Reason=strings.Replace(linearray[1], ")", "", -1)
				details.Ri = append(details.Ri, detailsRi)
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

	// Search if a kernel is apart of this update
	kernel := getkernelpkgname()
	details.KernelPkg = kernel
	details.KernelUp = false
	log.Println("Kernel: " + kernel)
	for i, _ := range details.Up {
		if ( details.Up[i].Name == kernel) {
			// Set JSON details on the kernel update
			details.KernelUp = true
                        break
		}
	}

	// Check if we have a SysUp package to update
	details.SysUpPkg = ""
	details.SysUp = false
	for i, _ := range details.Up {
		if ( details.Up[i].Name == "sysup") {
			// Set JSON details on the sysup package
			details.SysUp = true
                        break
		}
	}

	// If we have a remote ABI change we count that as a new kernel change also
        if ( haveosverchange() ) {
		details.KernelUp = true
	}

	//log.Print("UpdateInfo", details)
	return &details
}

func updatedryrun(sendupdate bool) (*UpdateInfo, bool, error) {
	details := UpdateInfo{ }
	updetails := &details

	cmd := exec.Command(PKGBIN, "-C", localpkgconf, "upgrade", "-n")
	sendinfomsg("Checking system for updates")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendfatalmsg("Failed dry run of pkg upgrade")
		return updetails, false, errors.New("ERROR")
	}
	if err := cmd.Start(); err != nil {
		sendfatalmsg("Failed dry run of pkg upgrade")
		return updetails, false, errors.New("ERROR")
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and append content to the slice
	var allText []string
	for buff.Scan() {
		allText = append(allText, buff.Text()+"\n")
	}
	//log.Println(allText)
	// Pkg returns 0 on sucess and 1 on updates needed
	//if err := cmd.Wait(); err != nil {
	//	log.Fatal(err)
	//}

	haveupdates := ! strings.Contains(strings.Join((allText), "\n"), "Your packages are up to date")
	if ( haveupdates ) {
		updetails = parseupdatedata(allText)
	}

	return updetails, haveupdates, nil
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
	preparepkgconfig("")
	updatepkgdb("")
	updetails, haveupdates, uerr:= updatedryrun(true)
	if ( uerr != nil ) {
                destroymddev()
		return
	}

        // If we are using standalone update, cleanup
	destroymddev()

	sendupdatedetails(haveupdates, updetails)
}
