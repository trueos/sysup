package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"strings"
	"syscall"
)

var kernelpkg string = ""

func getkernelpkgname() string {
	log.Println("Checking kernel package name")
	kernfile, kerr := syscall.Sysctl("kern.bootfile")
	if ( kerr != nil ) {
		log.Fatal(kerr)
	}
	kernpkgout, perr := exec.Command(PKGBIN, "which", kernfile).Output()
	if ( perr != nil ) {
		log.Fatal(perr)
	}
	kernarray := strings.Split(string(kernpkgout), " ")
	if ( len(kernarray) < 6 ) {
		log.Fatal("Unable to determine kernel package name")
	}
	kernpkg := kernarray[5]
	kernpkg = strings.TrimSpace(kernpkg)
	cmd := exec.Command(PKGBIN, "query", "%n", string(kernpkg))
	kernpkgname, err := cmd.CombinedOutput()
	if err != nil {
	    fmt.Println(fmt.Sprint(err) + ": " + string(kernpkgname))
	    log.Fatal("ERROR")
	}
	kernel := strings.TrimSpace(string(kernpkgname))
	return kernel
}

func doupdate(updatefile string) {
	log.Println("updatefile: " + updatefile)

	// Setup the pkg config directory
	preparepkgconfig()

	// Update the package database
	updatepkgdb()

	// Check that updates are available
	details, haveupdates := updatedryrun(false)
	if ( ! haveupdates ) {
		sendfatalmsg("ERROR: No updates to install!")
		return
	}

	// Check host OS version
	checkosver()

	// Start downloading our files
	startfetch()

	// Search if a kernel is apart of this update
	kernel := getkernelpkgname()
	var kernelupdate = false
        for i, _ := range details.Up {
		if ( details.Up[i].Name == kernel) {
			kernelupdate = true
			kernelpkg = kernel
			break
		}
        }

	// Start the upgrade
	startupgrade(kernelupdate)

	//os.Exit(0)
}

func cleanupbe() {
	cmd := exec.Command("umount", "-f", STAGEDIR + "/dev")
	cmd.Run()
	cmd = exec.Command("umount", "-f", STAGEDIR)
	cmd.Run()
	cmd = exec.Command(BEBIN, "destroy", "-F", BESTAGE)
	cmd.Run()
}

func createnewbe() {
	// Start creating the new BE and mount it for package ops
	sendinfomsg("Creating new Boot-Environment")
	cmd := exec.Command(BEBIN, "create", BESTAGE)
	err := cmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
	}
	cmd = exec.Command(BEBIN, "mount", BESTAGE, STAGEDIR)
	err = cmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
	}
	cmd = exec.Command("mount", "-t", "devfs", "devfs", STAGEDIR + "/dev")
	err = cmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
	}
	cmd = exec.Command("rm", "-rf", STAGEDIR + "/var/db/pkg")
	err = cmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
	}

        // Copy over the existing local database
        srcDir := STAGEDIR + localpkgdb
        destDir := STAGEDIR + "/var/db/pkg"
        cpCmd := exec.Command("mv", srcDir, destDir)
        err = cpCmd.Run()
        if ( err != nil ) {
                log.Fatal(err)
        }

        // Update the config file
        fdata := `PKG_CACHEDIR: ` + localcachedir + `
IGNORE_OSVERSION: YES`
        ioutil.WriteFile(STAGEDIR + localpkgconf, []byte(fdata), 0644)
}

func updatekernel() {
	sendinfomsg("Starting stage 1 kernel update")

	// KPM 11/9/2018
	// Additionally we may need to do something to ensure we don't load port kmods here on reboot
	cmd := exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "upgrade", "-y", "-f", kernelpkg)
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
		line := buff.Text()
		sendinfomsg(line)
		allText = append(allText, line+"\n")
	}
        // Pkg returns 0 on sucess and 1 on updates needed
        if err := cmd.Wait(); err != nil {
              log.Fatal(err)
        }
	sendinfomsg("Finished stage 1 kernel update")

}

func startupgrade(kernelupdate bool) {

	cleanupbe()

	createnewbe()

	if ( kernelupdate || fullupdateflag) {
		updatekernel()
	} else {
		log.Println("Starting stage 2 package update...")
	}

}

func startfetch() error {

	var dependflag string
	if ( fullupdateflag ) {
		dependflag = "-d"
	}
	cmd := exec.Command(PKGBIN, "-C", localpkgconf, "fetch", "-y", "-u", dependflag)
	sendinfomsg("Starting package downloads")
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
		line := buff.Text()
		sendinfomsg(line)
		allText = append(allText, line+"\n")
	}
        // Pkg returns 0 on sucess and 1 on updates needed
        if err := cmd.Wait(); err != nil {
              log.Fatal(err)
        }
	sendinfomsg("Finished package downloads")

        return nil
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
		fullupdateflag = true
	}
}
