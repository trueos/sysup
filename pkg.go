package main

import (
	"bufio"
	"fmt"
	"log"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
)

func getremoteosver() (string, error) {

	cmd := exec.Command(PKGBIN, "-C", localpkgconf, "rquery", "%At=%Av", "ports-mgmt/pkg")
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
	cmd := exec.Command(PKGBIN, "-C", localpkgconf, "update", "-f")
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
