package pkg

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/trueos/sysup/defines"
	"github.com/trueos/sysup/logger"
	"github.com/trueos/sysup/ws"
	"log"
	"syscall"
)

func sendupdatedetails(haveupdates bool, updetails *defines.UpdateInfo) {
	type JSONReply struct {
		Method  string              `json:"method"`
		Updates bool                `json:"updates"`
		Details *defines.UpdateInfo `json:"details"`
	}

	data := &JSONReply{
		Method:  "check",
		Updates: haveupdates,
		Details: updetails,
	}

	msg, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed encoding JSON:", err)
	}
	if err := defines.WSServer.WriteMessage(
		websocket.TextMessage, msg,
	); err != nil {
		log.Fatal(err)
	}
}

func CheckForUpdates() {
	PreparePkgConfig("")
	UpdatePkgDb("")
	updetails, haveupdates, uerr := UpdateDryRun(true)
	if uerr != nil {
		DestroyMdDev()
		return
	}

	// If we are using standalone update, cleanup
	DestroyMdDev()

	sendupdatedetails(haveupdates, updetails)
}

func HaveOsVerChange() bool {
	// Check the host OS version
	logger.LogToFile("Checking OS version")
	OSINT, oerr := syscall.SysctlUint32("kern.osreldate")
	if oerr != nil {
		log.Fatal(oerr)
	}
	REMOTEVER, err := GetRemoteOsVer()
	if err != nil {
		log.Fatal(err)
	}

	OSVER := fmt.Sprint(OSINT)
	logger.LogToFile("OS Version: " + OSVER + " -> " + REMOTEVER)
	if OSVER != REMOTEVER {
		ws.SendInfoMsg(
			"Remote ABI change detected: " + OSVER + " -> " + REMOTEVER,
		)
		logger.LogToFile(
			"Remote ABI change detected: " + OSVER + " -> " + REMOTEVER,
		)
		return true
	}
	return false
}
