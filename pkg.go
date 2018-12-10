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

	cmd := exec.Command(PKGBIN, "-C", localpkgconf, "rquery", "-U", "%At=%Av", "ports-mgmt/pkg")
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
		exitcleanup(err, "Failed getting remote version of ports-mgmt/pkg: " + strings.Join(allText, "\n"))
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

func mountofflineupdate() {

	// If offline update is already mounted, return
	if ( localmddev != "" ) {
		logtofile("Using already mounted: " + updatefileflag)
		return
	}

	if _, err := os.Stat(updatefileflag) ; os.IsNotExist(err) {
		sendfatalmsg("ERROR: Offline update file " + updatefileflag + " does not exist!")
		closews()
		os.Exit(1)
	}

	logtofile("Mounting offline update: " + updatefileflag)

	output, cmderr := exec.Command("mdconfig", "-a", "-t", "vnode", "-f", updatefileflag).Output()
	if ( cmderr != nil ) {
		exitcleanup(cmderr, "Failed mdconfig of offline update file: " + updatefileflag)
	}

	// Set the mddevice we have mounted
	localmddev := strings.TrimSpace(string(output))
	//log.Println("Local MD device: " + localmddev)

	derr := os.MkdirAll(localimgmnt, 0755)
	if derr != nil {
		log.Fatal(derr)
	}

	cmd := exec.Command("umount", "-f", localimgmnt)
	cmd.Run()

	// Mount the image RO
        cmd = exec.Command("mount", "-o", "ro", "/dev/" + localmddev, localimgmnt)
	err := cmd.Run()
	if ( err != nil ) {
		// We failed to mount, cleanup the memory device
		cmd := exec.Command("mdconfig", "-d", "-u", localmddev)
		cmd.Run()
		sendfatalmsg("ERROR: Offline update file " + updatefileflag + " cannot be mounted")
		localmddev=""
		closews()
		os.Exit(1)
	}
}

func destroymddev() {
	if ( updatefileflag == "" ) {
		return
	}
	cmd := exec.Command("umount", "-f", localimgmnt)
	cmd.Run()
        cmd = exec.Command("mdconfig", "-d", "-u", localmddev)
	cmd.Run()
	localmddev=""
}

func mkreposfile(prefix string, pkgdb string) string {
	reposdir := "REPOS_DIR: [ \"" + pkgdb + "/repos\", ]"
	rerr := os.MkdirAll(prefix + pkgdb + "/repos", 0755)
	if rerr != nil {
		log.Fatal(rerr)
	}
	// Ugly I know, can probably be re-factored later
	pkgdata := `Update: {
url: file:///` + localimgmnt
	if ( updatekeyflag != "" ) {
		pkgdata += `
  signature_type: "pubkey"
  pubkey: "`+ updatekeyflag + `
`
	} else {
		pkgdata += `
  signature_type: "none"
`
	}
	pkgdata += `
  enabled: yes
}`
	ioutil.WriteFile(prefix + pkgdb + "/repos/repo.conf", []byte(pkgdata), 0644)
	return reposdir
}

func preparepkgconfig() {
	derr := os.MkdirAll(localpkgdb, 0755)
	if derr != nil {
		exitcleanup(derr, "Failed making directory: " + localpkgdb)
	}
	cerr := os.MkdirAll(localcachedir, 0755)
	if cerr != nil {
		exitcleanup(cerr, "Failed making directory: " + localcachedir)
	}

	// If we have an offline file update, lets set that up now
	var reposdir string
	if ( updatefileflag != "" ) {
		mountofflineupdate()
		reposdir = mkreposfile("", localpkgdb)
	}

	// Copy over the existing local database
	srcFolder := "/var/db/pkg/local.sqlite"
	destFolder := localpkgdb + "/local.sqlite"
	cpCmd := exec.Command("cp", "-f", srcFolder, destFolder)
	err := cpCmd.Run()
	if ( err != nil ) {
		exitcleanup(err, "Failed copy of /var/db/pkg/local.sqlite")
	}

	// Create the config file
	fdata := `PKG_CACHEDIR: ` + localcachedir + `
PKG_DBDIR: ` + localpkgdb + `
IGNORE_OSVERSION: YES
` + reposdir
	ioutil.WriteFile(localpkgconf, []byte(fdata), 0644)
}

func updatepkgdb() {
	cmd := exec.Command(PKGBIN, "-C", localpkgconf, "update", "-f")
	sendinfomsg("Updating package remote database")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		exitcleanup(err, "Failed updating package remote DB")
	}
	if err := cmd.Start(); err != nil {
		exitcleanup(err, "Failed starting update of package remote DB")
	}
	buff := bufio.NewScanner(stdout)
	// Iterate over buff and append content to the slice
	var allText []string
	for buff.Scan() {
		allText = append(allText, buff.Text()+"\n")
	}
	//fmt.Println(allText)
	if err := cmd.Wait(); err != nil {
		exitcleanup(err, "Failed running pkg update:" + strings.Join(allText, "\n"))
	}
}

func exitcleanup(err error, text string) {
        // If we are using standalone update, cleanup
        if ( updatefileflag != "" && localmddev != "" ) {
                destroymddev()
        }
	log.Println("ERROR: " + text)
	log.Fatal(err)
}
