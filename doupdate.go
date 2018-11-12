package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"strconv"
	"syscall"
	"time"
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
        // Pkg returns 0 on sucess
        if err := cmd.Wait(); err != nil {
              log.Fatal(err)
        }
	sendinfomsg("Finished stage 1 kernel update")

}

func updateincremental(chroot bool, force bool, usingws bool) {
	if ( usingws ) {
		sendinfomsg("Starting stage 2 package update")
	} else {
		log.Println("Starting stage 2 package update")
	}

	var forceflag string
	if ( force ) {
		forceflag="-f"
	}

	cmd := exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "upgrade", "-y", forceflag)
	if ( ! chroot ) {
		cmd = exec.Command(PKGBIN, "-C", localpkgconf, "upgrade", "-y", forceflag)
	}
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
		if ( usingws ) {
			sendinfomsg(line)
		} else {
			log.Println(line)
		}
		allText = append(allText, line+"\n")
	}
        // Pkg returns 0 on sucess
        if err := cmd.Wait(); err != nil {
              log.Fatal(err)
        }
	if ( usingws ) {
		sendinfomsg("Finished stage 2 package update")
	}

}

func updatercscript() {
        // Intercept the /etc/rc script
        src := STAGEDIR + "/etc/rc"
        dest := STAGEDIR + "/etc/rc-updatergo"
        cpCmd := exec.Command("mv", src, dest)
	err := cpCmd.Run()
        if ( err != nil ) {
                log.Fatal(err)
        }

	var fuflag string
	if ( fullupdateflag ) {
		fuflag="-fullupdate"

	}

	selfbin, _ := os.Executable()
        ugobin := "/.update-go"
        cpCmd = exec.Command("install", "-m", "755", selfbin, STAGEDIR + ugobin)
	err = cpCmd.Run()
        if ( err != nil ) {
                log.Fatal(err)
        }

	// Splat down our intercept
        fdata := `#!/bin/sh
PATH="/sbin:/bin:/usr/sbin:/usr/bin:/usr/local/sbin:/usr/local/bin"
export PATH
` + ugobin + ` -stage2 ` + fuflag
        ioutil.WriteFile(STAGEDIR + "/etc/rc", []byte(fdata), 0755)

}

func startupgrade(kernelupdate bool) {

	cleanupbe()

	createnewbe()

	if ( kernelupdate || fullupdateflag) {
		updatekernel()
	} else {
		updateincremental(true, fullupdateflag, true)
	}

	updatercscript()
	renamebe()
	sendinfomsg("Success! Reboot your system to continue the update process.")
}

func renamebe() {
	curdate := time.Now()
	year := curdate.Year()
	month := int(curdate.Month())
	day := curdate.Day()
	hour := curdate.Hour()
	min := curdate.Minute()
	sec := curdate.Second()

	BENAME := strconv.Itoa(year) + "-" + strconv.Itoa(month) + "-" + strconv.Itoa(day) + "-" + strconv.Itoa(hour) + "-" + strconv.Itoa(min) + "-" + strconv.Itoa(sec)

	if ( benameflag != "" ) {
		BENAME = benameflag
	}

	// Write the new BENAME
        fdata := BENAME
        ioutil.WriteFile(STAGEDIR + "/.updategobename", []byte(fdata), 0644)

	// Start by unmounting BE
	cmd := exec.Command("beadm", "umount", "-f", BESTAGE)
	err := cmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
	}

	// Now rename BE
	cmd = exec.Command("beadm", "rename", BESTAGE , BENAME)
	err = cmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
	}

	// Get the current BE root
	shellcmd := "mount | awk '/ \\/ / {print $1}'"
	output, cmderr := exec.Command("/bin/sh", "-c", shellcmd).Output()
	if ( cmderr != nil ) {
		log.Fatal("Failed determining ZFS root")
	}
	currentbe := output
        linearray := strings.Split(string(currentbe), "/")
        if ( len(linearray) < 2) {
		log.Fatal("Invalid beroot: " + string(currentbe))
        }
	beroot := linearray[0] + "/" + linearray[1]

	// Lastly setup a one-time boot of this new BE
	// This should be zfsbootcfg at some point...
	cmd = exec.Command("beadm", "activate", BENAME)
	err = cmd.Run()
	if ( err != nil ) {
		log.Fatal("Failed activating: " + BENAME + " " + beroot)
	}
}

func startstage2() {
	// Need to ensure ZFS is all mounted and ready
	cmd := exec.Command("mount", "-u", "rw", "/")
	err := cmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
	}

	cmd = exec.Command("zfs", "mount", "-a")
	err = cmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
	}

	updateincremental(false, fullupdateflag, false)

	activatebe()
}

func activatebe() {
	dat, err := ioutil.ReadFile("/.updategobename")
	if ( err != nil ) {
		log.Fatal(err)
	}

	// Put back /etc/rc
        src := "/etc/rc-updatergo"
        dest := "/etc/rc"
        cpCmd := exec.Command("mv", src, dest)
	err = cpCmd.Run()
        if ( err != nil ) {
                log.Fatal(err)
        }

	// Activate the boot-environment
	bename := string(dat)
	cmd := exec.Command("beadm", "activate", bename)
	err = cmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
	}

	// Lastly reboot into the new environment
	cmd = exec.Command("reboot")
	err = cmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
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
