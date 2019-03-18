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
	kernpkgname, err := cmd.Output()
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
		SendReq
	}
	if err := json.Unmarshal(message, &s); err != nil {
		log.Fatal(err)
	}

	fullupdateflag = s.Fullupdate
	cachedirflag = s.Cachedir
	benameflag = s.Bename
	disablebsflag = s.Disablebs
	updatefileflag = s.Updatefile
	updatekeyflag = s.Updatekey
	//log.Println("benameflag: " + benameflag)
	//log.Println("updatefile: " + updatefileflag)

	// Start a fresh log file
	rotatelog()

	// Setup the pkg config directory
	logtofile("Setting up pkg database")
	preparepkgconfig("")

	// Update the package database
	logtofile("Updating package repo database")
	updatepkgdb("")

	// Check that updates are available
	logtofile("Checking for updates")
	details, haveupdates, uerr := updatedryrun(false)
	if ( uerr != nil ) {
		return
	}
	if ( ! haveupdates ) {
		sendfatalmsg("ERROR: No updates to install!")
		return
	}

	// Check host OS version
	logtofile("Checking OS version")
	if ( haveosverchange() ) {
		fullupdateflag = true
	}

	// Start downloading our files if we aren't doing stand-alone upgrade
	if ( updatefileflag == "" ) {
		logtofile("Fetching file updates")
		startfetch()
	}

	// If we have a sysup package we intercept here, do boot-strap and
	// Then restart the update with the fresh binary on a new port
	//
	// Skip if the disablebsflag is set
	if ( details.SysUp && disablebsflag != true) {
		logtofile("Performing bootstrap")
		dosysupbootstrap()
		dopassthroughupdate()
		return
	}

	// Start the upgrade
	startupgrade()

}

// This is called after a sysup boot-strap has taken place
//
// We will restart the sysup daemon on a new port and continue
// with the same update as previously requested
func dopassthroughupdate() {
	var fuflag string
	if ( fullupdateflag ) {
		fuflag="-fullupdate"

	}
	var cacheflag string
	if ( cachedirflag != "" ) {
		cacheflag="-cachedir=" + cachedirflag

	}
	var upflag string
	if ( updatefileflag != "" ) {
		upflag="-updatefile=" + updatefileflag
	}
	var beflag string
	if ( benameflag != "" ) {
		beflag="-bename=" + benameflag
	}
	var ukeyflag string
	if ( updatekeyflag != "" ) {
		ukeyflag="-updatekey=" + updatekeyflag
	}
	var wsflag string
	wsflag = "-addr=127.0.0.1:8135"

	// Start the newly updated sysup binary, passing along our previous flags
	//upflags := fuflag + " " + upflag + " " + beflag + " " + ukeyflag
	cmd := exec.Command("sysup", wsflag, "-update")
	if ( fuflag != "" ) {
		cmd.Args = append(cmd.Args, fuflag)
	}
	if ( cacheflag != "" ) {
		cmd.Args = append(cmd.Args, cacheflag)
	}
	if ( upflag != "" ) {
		cmd.Args = append(cmd.Args, upflag)
	}
	if ( beflag != "" ) {
		cmd.Args = append(cmd.Args, beflag)
	}
	if ( ukeyflag != "" ) {
		cmd.Args = append(cmd.Args, ukeyflag)
	}
	logtofile("Running bootstrap with flags: " + strings.Join(cmd.Args, " "))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and report back to listeners
	for buff.Scan() {
		sendinfomsg(buff.Text())
	}
        // Sysup returns 0 on sucess
        if err := cmd.Wait(); err != nil {
		sendfatalmsg("Failed update!")
        }

	// Let our local clients know they can finish up
	sendshutdownmsg("")
}

func doupdatefileumnt(prefix string) {
	if ( updatefileflag == "" ) {
		return
	}

	logtofile("Unmount nullfs")
	cmd := exec.Command("umount", "-f", prefix + localimgmnt)
	err := cmd.Run()
	if ( err != nil ) {
		log.Println("WARNING: Failed to umount " + prefix + localimgmnt)
	}
}

func doupdatefilemnt() {
	// If we are using standalone update need to nullfs mount the pkgs
	if ( updatefileflag == "" ) {
		return
	}

	logtofile("Mounting nullfs")
	cmd := exec.Command("mount_nullfs", localimgmnt, STAGEDIR + localimgmnt)
	err := cmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
	}
	logtofile("Nullfs mounted at: " + localimgmnt)
}

// When we have a new version of sysup to upgrade to, we perform
// that update first, and then continue with the regular update
func dosysupbootstrap() {

	// Start by updating the sysup PKG
	sendinfomsg("Starting Sysup boot-strap")
	logtofile("SysUp Stage 1\n-----------------------")

	// We update sysup command directly on the host
	// Why you may ask? Its written in GO for a reason
	// This allows us to run the new GO binaries on the system without worrying
	// about pesky library or ABI issues, horray!
	cmd := exec.Command(PKGBIN, "-C", localpkgconf, "upgrade", "-U", "-y", "-f", "sysup")
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
		sendinfomsg(line)
		logtofile(line)
	}
        // Pkg returns 0 on sucess
        if err := cmd.Wait(); err != nil {
		sendfatalmsg("Failed sysup update!")
        }

	cmd = exec.Command("rm", "-rf", "/var/db/pkg")
	err = cmd.Run()
	if ( err != nil ) {
		log.Fatal(err)
	}

        // Copy over the existing local database
        srcDir := localpkgdb
        destDir := "/var/db/pkg"
        cpCmd := exec.Command("mv", srcDir, destDir)
        err = cpCmd.Run()
        if ( err != nil ) {
                log.Fatal(err)
        }

	sendinfomsg("Finished stage 1 Sysup boot-strap")
	logtofile("FinishedSysUp Stage 1\n-----------------------")

	doupdatefileumnt("")
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
	logtofile("Creating new boot-environment")
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
		logtofile("Failed cleanup of: " + STAGEDIR + "/var/db/pkg")
		log.Fatal(err)
	}

	// On FreeNAS /etc/pkg is a nullfs memory system, and we want to catch
	// if any local changes have been made here which aren't yet on the new BE
	cmd = exec.Command("rm", "-rf", STAGEDIR + "/etc/pkg")
	err = cmd.Run()
	if ( err != nil ) {
		logtofile("Failed cleanup of: " + STAGEDIR + "/etc/pkg")
		log.Fatal(err)
	}
	cmd = exec.Command("cp", "-r", "/etc/pkg", STAGEDIR + "/etc/pkg")
	err = cmd.Run()
	if ( err != nil ) {
		logtofile("Failed copy of: /etc/pkg " + STAGEDIR + "/etc/pkg")
		log.Fatal(err)
	}

        // Copy over the existing local database
        srcDir := localpkgdb
        destDir := STAGEDIR + "/var/db/pkg"
        cpCmd := exec.Command("cp", "-r", srcDir, destDir)
        err = cpCmd.Run()
        if ( err != nil ) {
		logtofile("Failed copy of: " + localpkgdb + " -> " + STAGEDIR + "/var/db/pkg")
                log.Fatal(err)
        }

	var reposdir string
	if ( updatefileflag != "" ) {
		reposdir = mkreposfile(STAGEDIR, "/var/db/pkg")
	}

        // Update the config file
        fdata := `PKG_CACHEDIR: ` + localcachedir + `
IGNORE_OSVERSION: YES` + `
` + reposdir + `
` + abioverride
        ioutil.WriteFile(STAGEDIR + localpkgconf, []byte(fdata), 0644)
	logtofile("Done creating new boot-environment")
}

func sanitize_zfs() {

	// If we have a base system ZFS, we need to check if the port needs removing
	_, err := os.Stat(STAGEDIR + "/boot/modules/zfs.ko")
	if ( err == nil ) {
		cleanup_zol_port()
	}
}

func cleanup_zol_port() {

	sendinfomsg("Cleaning ZFS Port")
	logtofile("ZFS Port cleanup stage 1\n-----------------------")

	// Update the sysutils/zol port
	cmd := exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "delete", "-U", "-y", "sysutils/zol-kmod")
	logtofile("Cleaning up ZFS port with: " + strings.Join(cmd.Args, " "))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}

	stderr, err := cmd.StderrPipe()
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
		errbuf, _:= ioutil.ReadAll(stderr)
		errarr := strings.Split(string(errbuf), "\n")
		for i, _ := range errarr {
			sendinfomsg(errarr[i])
			logtofile(errarr[i])
		}
        }
	sendinfomsg("Finished stage 1 ZFS update")
	logtofile("Finished ZFS port cleanup stage 1\n-----------------------")
}

func checkbaseswitch() {

	// Does the new pkg repo have os/userland port origin
	cmd := exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "rquery", "-U", "%v", "os/userland")
	err := cmd.Run()
	if ( err != nil ) {
		return
	}

	// We have os/userland remote, lets see if we are using it already locally
	cmd = exec.Command(PKGBIN, "query", "%v", "os/userland")
	err = cmd.Run()
	if ( err == nil ) {
		return
	}

	output, cmderr := exec.Command("tar", "czf", STAGEDIR + "/.etcbackup.tgz", "-C", STAGEDIR + "/etc", ".").Output()
	if ( cmderr != nil ) {
		sendinfomsg(string(output))
		sendfatalmsg("Failed saving /etc configuration")
	}
	// Make sure pkg is fetched
	sendinfomsg("Fetching new base")
	logtofile("Fetching new base")
	cmd = exec.Command(PKGBIN, "-C", localpkgconf, "fetch", "-U", "-d", "-y", "os/userland", "os/kernel")
	err = cmd.Run()
	if ( err != nil ) {
		sendfatalmsg("Failed fetching new base")
	}

	// Get list of packages we need to nuke
	shellcmd := PKGBIN + " query '%o %n-%v' | grep '^base ' | awk '{print $2}'"
	output, cmderr = exec.Command("/bin/sh", "-c", shellcmd).Output()
	if ( cmderr != nil ) {
		sendfatalmsg("Failed getting base package list")
	}

	basepkgs := strings.TrimSpace(string(output))
	barr := strings.Split(basepkgs, "\n")
	for i, _ := range barr {
		// Unset vital flag
		sendinfomsg("Removing: " + barr[i])
		logtofile("Removing: " + barr[i])
		cmd := exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "set", "-v", "0", barr[i])
		err := cmd.Run()
		if ( err != nil ) {
			log.Fatal("Failed unsetting vital")
		}
		// Remove the package now
		cmd = exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "delete", "-y", "-f", "-g", barr[i])
		remout, err := cmd.CombinedOutput()
		if ( err != nil ) {
			sendinfomsg(string(remout))
			sendfatalmsg("Failed removing " + barr[i])
		}
		if ( strings.Contains(barr[i], "-runtime-1")) {
			// If this was the runtime package, need to re-install userland right away
			sendinfomsg("Boot-strapping userland")
			pkgcmd := exec.Command(PKGBIN, "-r", STAGEDIR, "-C", localpkgconf, "install", "-U", "-f", "-y", "os/userland")
			fullout, err := pkgcmd.CombinedOutput()
			if ( err != nil ) {
				sendinfomsg(string(fullout))
				sendfatalmsg("Failed boot-strapping userland")
			}
		}
	}

	// Load new userland / kernel and friends
	pkgcmd := exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "install", "-U", "-y", "os/userland", "os/kernel")
	fullout, err := pkgcmd.CombinedOutput()
	sendinfomsg(string(fullout))
	logtofile(string(fullout))

	// Ensure pkg is boot-strapped again
	pkgcmd = exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "install", "-U", "-y", "ports-mgmt/pkg")
	fullout, err = pkgcmd.CombinedOutput()
	sendinfomsg(string(fullout))
	logtofile(string(fullout))

	// Copy back the /etc changes
	output, cmderr = exec.Command("tar", "xf", STAGEDIR + "/.etcbackup.tgz", "-C", STAGEDIR + "/etc").CombinedOutput()
	if ( cmderr != nil ) {
		sendinfomsg(string(output))
		sendinfomsg("WARNING: Tar error while updating /etc configuration")
	}
	exec.Command("rm", STAGEDIR + "/.etcbackup.tgz").Run()

	// Make sure sysup is marked as installed
	pkgcmd = exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "set", "-y", "-A", "00", "sysutils/sysup")
	fullout, err = pkgcmd.CombinedOutput()
	sendinfomsg(string(fullout))
	logtofile(string(fullout))
}

func updateincremental(force bool) {
	sendinfomsg("Starting package update")
	logtofile("PackageUpdate\n-----------------------")

	// Check if we are moving from legacy pkg base to ports-base
	checkbaseswitch()

	pkgcmd := exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "upgrade", "-U", "-y", "-f", "ports-mgmt/pkg")
	fullout, err := pkgcmd.CombinedOutput()
	sendinfomsg(string(fullout))
	logtofile(string(fullout))

	// Setup our main update process
	cmd := exec.Command(PKGBIN)
	cmd.Args = append(cmd.Args, "-c")
	cmd.Args = append(cmd.Args, STAGEDIR)
	cmd.Args = append(cmd.Args, "-C")
	cmd.Args = append(cmd.Args, localpkgconf)
	cmd.Args = append(cmd.Args, "upgrade")
	cmd.Args = append(cmd.Args, "-U")
	cmd.Args = append(cmd.Args, "-I")
	cmd.Args = append(cmd.Args, "-y")

	// Reinstall everything?
	if ( force ) {
		cmd.Args = append(cmd.Args, "-f")
	}
	logtofile("Starting upgrade with: " + strings.Join(cmd.Args, " "))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		destroymddev()
		logtofile("Failed starting pkg upgrade stdout!")
		sendfatalmsg("Failed starting pkg upgrade stdout!")
	}
	if err := cmd.Start(); err != nil {
		destroymddev()
		logtofile("Failed starting pkg upgrade!")
		sendfatalmsg("Failed starting pkg upgrade!")
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and append content to the slice
	var allText []string
	for buff.Scan() {
		line := buff.Text()
		sendinfomsg(line)
		logtofile("pkg: " + line)
		allText = append(allText, line+"\n")
	}
        // Pkg returns 0 on sucess
        if err := cmd.Wait(); err != nil {
		destroymddev()
		logtofile("Failed pkg upgrade!")
		sendfatalmsg("Failed pkg upgrade!")
        }
	sendinfomsg("Finished stage package update")
	logtofile("FinishedPackageUpdate\n-----------------------")

	// Mark essential pkgs
	critpkg := []string { "ports-mgmt/pkg", "os/userland", "os/kernel" }
	for i, _ := range critpkg {
		pkgcmd = exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "set", "-y", "-A", "00", critpkg[i])
		fullout, err = pkgcmd.CombinedOutput()
		sendinfomsg(string(fullout))
		logtofile(string(fullout))
	}

	// Cleanup orphans
	pkgcmd = exec.Command(PKGBIN, "-c", STAGEDIR, "-C", localpkgconf, "autoremove", "-y")
	fullout, err = pkgcmd.CombinedOutput()
	sendinfomsg(string(fullout))
	logtofile(string(fullout))

}

func startupgrade() {

	cleanupbe()

	createnewbe()

	// If we are using standalone update need to nullfs mount the pkgs
	doupdatefilemnt()

	updateincremental(fullupdateflag)

	// Check if we need to do any ZFS automagic
	sanitize_zfs()

	// Update the boot-loader
	updateloader(STAGEDIR)

	// Cleanup nullfs mount
	doupdatefileumnt(STAGEDIR)

	// Unmount the devfs point
	cmd := exec.Command("umount", "-f", STAGEDIR + "/dev")
	cmd.Run()

	// Rename to proper BE name
	renamebe()

	// If we are using standalone update, cleanup
	destroymddev()
	sendshutdownmsg("Success! Reboot your system to continue the update process.")
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

	sendfatalmsg("Aborting")
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

	cmd := exec.Command(PKGBIN, "-C", localpkgconf, "upgrade", "-F", "-y", "-U")

	// Are we doing a full update?
	if ( fullupdateflag ) {
		cmd.Args = append(cmd.Args, "-f")
	}

	sendinfomsg("Starting package downloads")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
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
        // If we get a non-0 back, report the full error
        if err := cmd.Wait(); err != nil {
		errbuf, _:= ioutil.ReadAll(stderr)
		errarr := strings.Split(string(errbuf), "\n")
		for i, _ := range errarr {
			sendinfomsg(errarr[i])
		}
		sendfatalmsg("Failed package fetch!")
		return err
        }
	sendinfomsg("Finished package downloads")

        return nil
}

func haveosverchange() bool {
	// Check the host OS version
	logtofile("Checking OS version")
	OSINT, oerr := syscall.SysctlUint32("kern.osreldate")
	if ( oerr != nil ) {
		log.Fatal(oerr)
	}
	REMOTEVER, err := getremoteosver()
	if ( err != nil ) {
		log.Fatal(err)
	}

	OSVER := fmt.Sprint(OSINT)
	logtofile("OS Version: " + OSVER + " -> " + REMOTEVER)
	if ( OSVER != REMOTEVER ) {
		sendinfomsg("Remote ABI change detected: " +OSVER+ " -> " + REMOTEVER )
		logtofile("Remote ABI change detected: " +OSVER+ " -> " + REMOTEVER )
		return true
	}
	return false
}

func updateloader(stagedir string) {
	logtofile("Updating Bootloader\n-------------------")
	sendinfomsg("Updating Bootloader")
	disks := getzpooldisks()
	for i, _ := range disks {
		if (isuefi(disks[i])) {
			logtofile("Updating EFI boot-loader on: " + disks[i])
			sendinfomsg("Updating EFI boot-loader on: " + disks[i])
			if ( ! updateuefi(disks[i], stagedir) ) {
				sendfatalmsg("Updating boot-loader failed!")
			}
		} else {
			logtofile("Updating GPT boot-loader on: " + disks[i])
			sendinfomsg("Updating GPT boot-loader on: " + disks[i])
			if ( ! updategpt(disks[i], stagedir) ) {
				sendfatalmsg("Updating boot-loader failed!")
			}
		}
	}
}

func updateuefi(disk string, stagedir string) bool {
        derr := os.MkdirAll("/boot/efi", 0755)
        if derr != nil {
		sendinfomsg("ERROR: Failed mkdir /boot/efi")
		copylogexit(derr, "Failed mkdir /boot/efi")
        }

	cmd := exec.Command("gpart", "show", disk)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendinfomsg("ERROR: Failed gpart show")
		copylogexit(derr, "Failed gpart show")
	}
	if err := cmd.Start(); err != nil {
		sendinfomsg("ERROR: Failed starting gpart show")
		copylogexit(derr, "Failed starting gpart show")
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and look for specific boot partition
	for buff.Scan() {
		line := strings.TrimSpace(buff.Text())
		if ( strings.Contains(line, " efi ") ) {
			linearray := strings.Fields(line)
			if ( len(linearray) < 3) {
				sendinfomsg("ERROR: Unable to locate EFI partition")
				logtofile("Unable to locate EFI partition..." + string(line))
				return false
			}
			part := linearray[2]

			// Mount the UEFI partition
			bcmd := exec.Command("mount", "-t", "msdosfs", "/dev/" + disk + "p" +  part, "/boot/efi")
			berr := bcmd.Run()
			if berr != nil {
				sendinfomsg("ERROR: Unable to mount EFI partition")
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
			cmd := exec.Command("cp", stagedir + "/boot/loader.efi", tgt)
			cerr := cmd.Run()
			if cerr != nil {
				sendinfomsg("ERROR: Unable to copy efi file " + tgt)
				logtofile("Unable to copy efi file: " + tgt)
				return false
			}

			// Unmount and cleanup
			bcmd = exec.Command("umount", "-f", "/boot/efi")
			berr = bcmd.Run()
			if berr != nil {
				sendinfomsg("ERROR: Unable to umount EFI pratition " + part)
				logtofile("Unable to umount EFI partition: " + part)
				return false
			}

			return true
		}
	}
	sendinfomsg("ERROR: Unable to locate EFI partition on: " + string(disk))
	logtofile("Unable to locate EFI partition on: " + string(disk))
	return false
}

func updategpt(disk string, stagedir string) bool {
	cmd := exec.Command("gpart", "show", disk)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendinfomsg("ERROR: Failed gpart show")
		copylogexit(err, "Failed gpart show")
	}
	if err := cmd.Start(); err != nil {
		sendinfomsg("ERROR: Failed starting gpart show")
		copylogexit(err, "Failed starting gpart show")
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and look for specific boot partition
	for buff.Scan() {
		line := strings.TrimSpace(buff.Text())
		if ( strings.Contains(line, " freebsd-boot ") ) {
			linearray := strings.Fields(line)
			if ( len(linearray) < 3) {
				sendinfomsg("ERROR: Failed to locate GPT boot partition...")
				logtofile("Unable to locate GPT boot partition..." + string(line))
				return false
			}
			part := linearray[2]
			bcmd := exec.Command("gpart", "bootcode", "-b", stagedir + "/boot/pmbr", "-p", stagedir + "/boot/gptzfsboot", "-i", part, disk)
			berr := bcmd.Run()
			if berr != nil {
				sendinfomsg("Failed gpart bootcode -b " + stagedir + "/boot/pmbr -p " + stagedir + "/boot/gptzfsboot -i " + part + " " + disk)
				copylogexit(berr, "Failed gpart bootcode -b " + stagedir + "/boot/pmbr -p " + stagedir + "/boot/gptzfsboot -i " + part + " " + disk)
			}
			return true
		}
	}
	sendinfomsg("Unable to locate freebsd-boot partition on: " + string(disk))
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
		if ( strings.Contains(line, " efi ") ) {
			return true
		}
		if ( strings.Contains(line, "freebsd-boot") ) {
			return false
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
		diskarr = append(diskarr, kerndisks[i])
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
