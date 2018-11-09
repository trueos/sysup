package main

import (
	"fmt"
	"log"
	"os"
	"syscall"
)

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
