package main

import (
	"bufio"
	"encoding/json"
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
	logtofile("Checking kernel package name")
	kernfile, kerr := syscall.Sysctl("kern.bootfile")
	if ( kerr != nil ) {
		logtofile("Failed getting kern.bootfile")
		log.Fatal(kerr)
	}
	kernpkgout, perr := exec.Command(PKGBIN, "which", kernfile).Output()
	if ( perr != nil ) {
		logtofile("Failed which " + kernfile)
		log.Fatal(perr)
	}
	kernarray := strings.Split(string(kernpkgout), " ")
	if ( len(kernarray) < 6 ) {
		logtofile("Unable to determine kernel package name")
		log.Fatal("Unable to determine kernel package name")
	}
	kernpkg := kernarray[5]
	kernpkg = strings.TrimSpace(kernpkg)
	logtofile("Local Kernel package: " + kernpkg)
	shellcmd := PKGBIN + " info " + kernpkg + " | grep '^Name' | awk '{print $3}'"
	cmd := exec.Command("/bin/sh", "-c", shellcmd)
	kernpkgname, err := cmd.CombinedOutput()
	if err != nil {
	    fmt.Println(fmt.Sprint(err) + ": " + string(kernpkgname))
	    log.Fatal("ERROR query of kernel package name")
	}
	kernel := strings.TrimSpace(string(kernpkgname))

	logtofile("Kernel package: " + kernel)
	return kernel
}

func doupdate(message []byte) {
	// Unpack the options for this update request
	var s struct {
		Envelope
		UpdateReq
	}
	if err := json.Unmarshal(message, &s); err != nil {
		log.Fatal(err)
	}

	fullupdateflag = s.Fullupdate
	benameflag = s.Bename
	updatefileflag = s.Updatefile
	updatekeyflag = s.Updatekey
	//log.Println("benameflag: " + benameflag)
	log.Println("updatefile: " + updatefileflag)

	// Start a fresh log file
	rotatelog()

	// Setup the pkg config directory
	logtofile("Setting up pkg database")
	preparepkgconfig()

	// Update the package database
	logtofile("Updating package repo database")
	updatepkgdb()

	// Check that updates are available
	logtofile("Checking for updates")
	details, haveupdates := updatedryrun(false)
	if ( ! haveupdates ) {
		sendfatalmsg("ERROR: No updates to install!")
		return
	}

	// Check host OS version
	logtofile("Checking OS version")
	checkosver()

	// Start downloading our files if we aren't doing stand-alone upgrade
	if ( updatefileflag == "" ) {
		logtofile("Fetching file updates")
		startfetch()
	}

	// Search if a kernel is apart of this update
	if ( details.KernelUp ) {
		kernelpkg = details.KernelPkg
	}

	// Start the upgrade with bool passed if doing kernel update
	startupgrade(details.KernelUp)

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

	var reposdir string
	if ( updatefileflag != "" ) {
		reposdir = mkreposfile(STAGEDIR, "/var/db/pkg")
	}

        // Update the config file
        fdata := `PKG_CACHEDIR: ` + localcachedir + `
IGNORE_OSVERSION: YES` + `
` + reposdir
        ioutil.WriteFile(STAGEDIR + localpkgconf, []byte(fdata), 0644)
}

func updatekernel() {
	sendinfomsg("Starting stage 1 kernel update")
	logtofile("KernelUpdate Stage 1\n-----------------------")

	// KPM 11/9/2018
	// Additionally we may need to do something to ensure we don't load port kmods here on reboot
	cmd := exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "upgrade", "-U", "-y", "-f", kernelpkg)
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
		logtofile(line)
		allText = append(allText, line+"\n")
	}
        // Pkg returns 0 on sucess
        if err := cmd.Wait(); err != nil {
		sendfatalmsg("Failed kernel update!")
        }
	sendinfomsg("Finished stage 1 kernel update")
	logtofile("FinishedKernelUpdate Stage 1\n-----------------------")

}

func updateincremental(chroot bool, force bool, usingws bool) {
	if ( usingws ) {
		sendinfomsg("Starting stage 2 package update")
	} else {
		log.Println("Starting stage 2 package update")
	}
	logtofile("PackageUpdate Stage 2\n-----------------------")

	cmd := exec.Command(PKGBIN)
	if ( force ) {
		cmd = exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "upgrade", "-U", "-y", "-f")
		if ( ! chroot ) {
			cmd = exec.Command(PKGBIN, "-C", localpkgconf, "upgrade", "-U", "-y", "-f")
		}
	} else {
		cmd = exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "upgrade", "-U", "-y")
		if ( ! chroot ) {
			cmd = exec.Command(PKGBIN, "-C", localpkgconf, "upgrade", "-U", "-y")
		}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		destroymddev()
		if ( usingws ) {
			logtofile("Failed starting pkg upgrade stdout!")
			sendfatalmsg("Failed starting pkg upgrade stdout!")
		} else {
			copylogexit(err, "Failed starting pkg upgrade stdout!")
		}
	}
	if err := cmd.Start(); err != nil {
		destroymddev()
		if ( usingws) {
			logtofile("Failed starting pkg upgrade!")
			sendfatalmsg("Failed starting pkg upgrade!")
		} else {
			copylogexit(err, "Failed starting pkg upgrade!")
		}
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
		logtofile("pkg: " + line)
		allText = append(allText, line+"\n")
	}
        // Pkg returns 0 on sucess
        if err := cmd.Wait(); err != nil {
		destroymddev()
		if ( usingws) {
			logtofile("Failed pkg upgrade!")
			sendfatalmsg("Failed pkg upgrade!")
		} else {
			copylogexit(err, "Failed pkg upgrade!")
		}
        }
	if ( usingws ) {
		sendinfomsg("Finished stage 2 package update")
	}
	logtofile("FinishedPackageUpdate Stage 2\n-----------------------")

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
	var upflag string
	if ( updatefileflag != "" ) {
		upflag="-updatefile " + updatefileflag
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
` + ugobin + ` -stage2 ` + fuflag + ` ` + upflag
        ioutil.WriteFile(STAGEDIR + "/etc/rc", []byte(fdata), 0755)

}

func startupgrade(kernelupdate bool) {

	cleanupbe()

	createnewbe()

	// If we are using standalone update need to nullfs mount the pkgs
	if ( updatefileflag != "" ) {
		logtofile("Mounting nullfs")
		cmd := exec.Command("mount_nullfs", localimgmnt, STAGEDIR + localimgmnt)
		err := cmd.Run()
		if ( err != nil ) {
			log.Fatal(err)
		}
	}


	if ( kernelupdate || fullupdateflag) {
		updatekernel()
		updatercscript()
	} else {
		updateincremental(true, fullupdateflag, true)
	}

	// Cleanup nullfs mount
	if ( updatefileflag != "" ) {
		logtofile("Unmount nullfs")
		cmd := exec.Command("umount", "-f", STAGEDIR + localimgmnt)
		err := cmd.Run()
		if ( err != nil ) {
			log.Fatal(err)
		}
	}

	// Unmount the devfs point
	cmd := exec.Command("umount", "-f", STAGEDIR + "/dev")
	cmd.Run()

	// Rename to proper BE name
	renamebe()

	// If we are using standalone update, cleanup
	destroymddev()
	sendinfomsg("Success! Reboot your system to continue the update process.")
	time.Sleep(500 * time.Millisecond)
	os.Exit(0)
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

	// Write the old BE name
        fdata := BENAME
        ioutil.WriteFile(STAGEDIR + "/.updategobename", []byte(fdata), 0644)

	// Write the old BE name
        odata := strings.TrimSpace(getcurrentbe())
        ioutil.WriteFile(STAGEDIR + "/.updategooldbename", []byte(odata), 0644)

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


	// Lastly setup a one-time boot of this new BE
	// This should be zfsbootcfg at some point...
	cmd = exec.Command("beadm", "activate", BENAME)
	err = cmd.Run()
	if ( err != nil ) {
		log.Fatal("Failed activating: " + BENAME)
	}

}

/*
* Something has gone horribly wrong, lets make a copy of the
* log file and reboot into the old BE for later debugging
*/
func copylogexit(perr error, text string) {

	logtofile("FAILED Upgrade!!!")
	logtofile(perr.Error())
	logtofile(text)
	log.Println(text)

	src := logfile
        dest := "/var/log/updatego.failed"
        cpCmd := exec.Command("cp", src, dest)
	cpCmd.Run()

	cmd := exec.Command("reboot")
	cmd.Run()
	os.Exit(0)
}

func preparestage2() {
	log.Println("Preparing to start update...")

	// Need to ensure ZFS is all mounted and ready
	cmd := exec.Command("mount", "-u", "rw", "/")
	err := cmd.Run()
	if ( err != nil ) {
		copylogexit(err, "Failed mounting -u rw")
	}

	// Set the OLD BE as the default in case we crash and burn...
	dat, err := ioutil.ReadFile("/.updategooldbename")
	if ( err != nil ) {
		copylogexit(err, "Failed read .updategooldbename")
	}

	bename := strings.TrimSpace(string(dat))
	// Now activate
	out, err := exec.Command("beadm", "activate", bename).CombinedOutput()
	if ( err != nil ) {
		copylogexit(err, "Failed beadm activate: " + bename + " " + string(out))
	}

	// Make sure everything is mounted and ready!
	cmd = exec.Command("zfs", "mount", "-a")
	err = cmd.Run()
	if ( err != nil ) {
		copylogexit(err, "Failed zfs mount -a")
	}

	// Need to try and kldload linux64 / linux so some packages can update
	cmd = exec.Command("kldload", "linux64")
	err = cmd.Run()
	if ( err != nil ) {
		logtofile("WARNING: unable to kldload linux64")
	}
	cmd = exec.Command("kldload", "linux")
	err = cmd.Run()
	if ( err != nil ) {
		logtofile("WARNING: unable to kldload linux")
	}

	// Put back /etc/rc-updatergo so that pkg can update it properly
	src := "/etc/rc-updatergo"
        dest := "/etc/rc"
        cpCmd := exec.Command("mv", src, dest)
	err = cpCmd.Run()
        if ( err != nil ) {
		copylogexit(err, "Failed restoring /etc/rc")
        }
}

func startstage2() {

	preparestage2()

	if ( updatefileflag != "" ) {
		mountofflineupdate()
	}

	updateincremental(false, fullupdateflag, false)

	destroymddev()

	// SUCCESS! Lets finish and activate the new BE
	activatebe()

	// Lastly lets update the loader
	updateloader()

	// Lastly reboot into the new environment
	cmd := exec.Command("reboot")
	err := cmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
	}

}

func activatebe() {
	dat, err := ioutil.ReadFile("/.updategobename")
	if ( err != nil ) {
		copylogexit(err, "Failed reading updategobename")
	}

	// Activate the boot-environment
	bename := string(dat)
	cmd := exec.Command("beadm", "activate", bename)
	err = cmd.Run()
	if ( err != nil ) {
		copylogexit(err, "Failed beadm activate: " + bename)
	}
}

func startfetch() error {

	var dependflag string
	if ( fullupdateflag ) {
		dependflag = "-f"
	}
	cmd := exec.Command(PKGBIN, "-C", localpkgconf, "upgrade", "-F", "-y", "-U", dependflag)
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

func updateloader() {
	logtofile("Updating Bootloader\n-------------------")
	disks := getzpooldisks()
	for i, _ := range disks {
		if (isuefi(disks[i])) {
			logtofile("Updating EFI boot-loader on: " + disks[i])
			updateuefi(disks[i])
		} else {
			logtofile("Updating GPT boot-loader on: " + disks[i])
			updategpt(disks[i])
		}
	}
}

func updateuefi(disk string) bool {
        derr := os.MkdirAll("/boot/efi", 0755)
        if derr != nil {
		copylogexit(derr, "Failed mkdir /boot/efi")
        }

	cmd := exec.Command("gpart", "show", disk)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		copylogexit(derr, "Failed gpart show")
	}
	if err := cmd.Start(); err != nil {
		copylogexit(derr, "Failed starting gpart show")
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and look for specific boot partition
	for buff.Scan() {
		line := strings.TrimSpace(buff.Text())
		if ( strings.Contains(line, " efi ") ) {
			linearray := strings.Split(string(line), " ")
			if ( len(linearray) < 3) {
				logtofile("Unable to locate EFI partition..." + string(line))
				return false
			}
			part := linearray[2]

			// Mount the UEFI partition
			bcmd := exec.Command("mount", "-t", "msdosfs", "/dev/" + disk + "p" +  part, "/boot/efi")
			berr := bcmd.Run()
			if berr != nil {
				logtofile("Unable to mount EFI partition: " + part)
				return false
			}

			// Copy the new UEFI file over
			var tgt string
			if _, err := os.Stat("/boot/efi/efi/boot/bootx64-trueos.efi") ; os.IsNotExist(err) {
				tgt = "/boot/efi/efi/boot/bootx64-trueos.efi"
			} else {
				tgt = "/boot/efi/efi/boot/bootx64.efi"
			}
			cmd := exec.Command("cp", "/boot/boot1.efi", tgt)
			cerr := cmd.Run()
			if cerr != nil {
				logtofile("Unable to copy efi file: " + tgt)
				return false
			}

			// Unmount and cleanup
			bcmd = exec.Command("umount", "-f", "/boot/efi")
			berr = bcmd.Run()
			if berr != nil {
				logtofile("Unable to umount EFI partition: " + part)
				return false
			}

			return true
		}
	}
	logtofile("Unable to locate EFI partition on: " + string(disk))
	return false
}


func updategpt(disk string) bool {
	cmd := exec.Command("gpart", "show", disk)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		copylogexit(err, "Failed gpart show")
	}
	if err := cmd.Start(); err != nil {
		copylogexit(err, "Failed starting gpart show")
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and look for specific boot partition
	for buff.Scan() {
		line := strings.TrimSpace(buff.Text())
		if ( strings.Contains(line, " freebsd-boot ") ) {
			linearray := strings.Split(string(line), " ")
			if ( len(linearray) < 3) {
				logtofile("Unable to locate GPT boot partition..." + string(line))
				return false
			}
			part := linearray[2]
			bcmd := exec.Command("gpart", "bootcode", "-b", "/boot/pmbr", "-p", "gptzfsboot", "-i", part)
			berr := bcmd.Run()
			if berr != nil {
				copylogexit(berr, "Failed gpart bootcode -b /boot/pmbr -p gptzfsboot -i " + part)
			}
			return true
		}
	}
	logtofile("Unable to locate freebsd-boot partition on: " + string(disk))
	return false
}

func isuefi(disk string) bool {
	cmd := exec.Command("gpart", "show", disk)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		copylogexit(err, "Failed gpart show (isuefi)")
	}
	if err := cmd.Start(); err != nil {
		copylogexit(err, "Failed starting gpart show (isuefiu)")
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and look for disk matches
	for buff.Scan() {
		line := buff.Text()
		if ( strings.Contains(line, "freebsd-boot") ) {
			return true
		}
	}
	return false
}

func getberoot() string {
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
	return beroot
}

func getcurrentbe() string {
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
	be := linearray[2]
	return be
}

func getzfspool() string {
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
	return linearray[0]
}

func getzpooldisks() []string {
	var diskarr []string
	zpool := getzfspool()
	kernout, kerr := syscall.Sysctl("kern.disks")
	if ( kerr != nil ) {
		log.Fatal(kerr)
	}
	kerndisks := strings.Split(string(kernout), " ")
	for i, _ := range kerndisks {
		// Yes, CD's show up in this output..
		if ( strings.Index(kerndisks[i], "cd") == 0) {
			continue
		}
		// Get a list of uuids for the partitions on this disk
		duuids := getdiskuuids(kerndisks[i])

		// Validate this disk is in the default zpool
		if ( ! diskisinpool(kerndisks[i], duuids, zpool) ) {
			continue
		}
		logtofile("Updating boot-loader on disk: " + kerndisks[i])
        }
	return diskarr
}

func diskisinpool(disk string, uuids []string, zpool string) bool {
	cmd := exec.Command("zpool", "status", zpool)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and look for disk matches
	for buff.Scan() {
		line := buff.Text()
		if ( strings.Contains(line, " " + disk + "p") ) {
			return true
		}
		for i, _ := range uuids {
			if ( strings.Contains(line, " gptid/" + uuids[i] ) ) {
				return true
			}
		}
	}
        if err := cmd.Wait(); err != nil {
              log.Fatal(err)
        }
	return false
}

func getdiskuuids(disk string) []string {
	var uuidarr []string
	shellcmd := "gpart list " + disk + " | grep rawuuid | awk '{print $2}'"
	cmd := exec.Command("/bin/sh", "-c", shellcmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and append content to the slice
	for buff.Scan() {
		line := buff.Text()
		uuidarr = append(uuidarr, line)
	}
        // Pkg returns 0 on sucess
        if err := cmd.Wait(); err != nil {
              log.Fatal(err)
        }

	return uuidarr
}
