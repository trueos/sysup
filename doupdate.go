package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/trueos/sysup/defines"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var kernelpkg string = ""

func getkernelpkgname() string {
	logtofile("Checking kernel package name")
	kernfile, kerr := syscall.Sysctl("kern.bootfile")
	if kerr != nil {
		logtofile("Failed getting kern.bootfile")
		log.Fatal(kerr)
	}
	kernpkgout, perr := exec.Command(defines.PKGBIN, "which", kernfile).Output()
	if perr != nil {
		logtofile("Failed which " + kernfile)
		log.Fatal(perr)
	}
	kernarray := strings.Split(string(kernpkgout), " ")
	if len(kernarray) < 6 {
		logtofile("Unable to determine kernel package name")
		log.Fatal("Unable to determine kernel package name")
	}
	kernpkg := kernarray[5]
	kernpkg = strings.TrimSpace(kernpkg)
	logtofile("Local Kernel package: " + kernpkg)
	shellcmd := defines.PKGBIN + " info " + kernpkg + " | grep '^Name' | awk '{print $3}'"
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
		defines.Envelope
		defines.SendReq
	}
	if err := json.Unmarshal(message, &s); err != nil {
		log.Fatal(err)
	}

	defines.FullUpdateFlag = s.Fullupdate
	defines.CacheDirFlag = s.Cachedir
	defines.BeNameFlag = s.Bename
	defines.DisableBsFlag = s.Disablebs
	defines.UpdateFileFlag = s.Updatefile
	defines.UpdateKeyFlag = s.Updatekey
	//log.Println("benameflag: " + benameflag)
	//log.Println("updatefile: " + updatefileflag)

	// Update any variable locations to use cachedirflag
	setlocs()

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
	if uerr != nil {
		return
	}
	if !haveupdates {
		sendfatalmsg("ERROR: No updates to install!")
		return
	}

	// Check host OS version
	logtofile("Checking OS version")
	if haveosverchange() {
		defines.FullUpdateFlag = true
	}

	// Start downloading our files if we aren't doing stand-alone upgrade
	if defines.UpdateFileFlag == "" {
		logtofile("Fetching file updates")
		startfetch()
	}

	// If we have a sysup package we intercept here, do boot-strap and
	// Then restart the update with the fresh binary on a new port
	//
	// Skip if the disablebsflag is set
	if details.SysUp && defines.DisableBsFlag != true {
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
	if defines.FullUpdateFlag {
		fuflag = "-fullupdate"

	}
	var cacheflag string
	if defines.CacheDirFlag != "" {
		cacheflag = "-cachedir=" + defines.CacheDirFlag

	}
	var upflag string
	if defines.UpdateFileFlag != "" {
		upflag = "-updatefile=" + defines.UpdateFileFlag
	}
	var beflag string
	if defines.BeNameFlag != "" {
		beflag = "-bename=" + defines.BeNameFlag
	}
	var ukeyflag string
	if defines.UpdateKeyFlag != "" {
		ukeyflag = "-updatekey=" + defines.UpdateKeyFlag
	}
	var wsflag string
	wsflag = "-addr=127.0.0.1:8135"

	// Start the newly updated sysup binary, passing along our previous flags
	//upflags := fuflag + " " + upflag + " " + beflag + " " + ukeyflag
	cmd := exec.Command("sysup", wsflag, "-update")
	if fuflag != "" {
		cmd.Args = append(cmd.Args, fuflag)
	}
	if cacheflag != "" {
		cmd.Args = append(cmd.Args, cacheflag)
	}
	if upflag != "" {
		cmd.Args = append(cmd.Args, upflag)
	}
	if beflag != "" {
		cmd.Args = append(cmd.Args, beflag)
	}
	if ukeyflag != "" {
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
	if defines.UpdateFileFlag == "" {
		return
	}

	logtofile("Unmount nullfs")
	cmd := exec.Command("umount", "-f", prefix+defines.ImgMnt)
	err := cmd.Run()
	if err != nil {
		log.Println("WARNING: Failed to umount " + prefix + defines.ImgMnt)
	}
}

func doupdatefilemnt() {
	// If we are using standalone update need to nullfs mount the pkgs
	if defines.UpdateFileFlag == "" {
		return
	}

	logtofile("Mounting nullfs")
	cmd := exec.Command(
		"mount_nullfs", defines.ImgMnt,
		defines.STAGEDIR+defines.ImgMnt,
	)
	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
	logtofile("NullFS mounted at: " + defines.ImgMnt)
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
	cmd := exec.Command(
		defines.PKGBIN, "-C", defines.PkgConf, "upgrade", "-U", "-y",
		"-f", "sysup",
	)
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
	if err != nil {
		log.Fatal(err)
	}

	// Copy over the existing local database
	srcDir := defines.PkgDb
	destDir := "/var/db/pkg"
	cpCmd := exec.Command("mv", srcDir, destDir)
	err = cpCmd.Run()
	if err != nil {
		log.Fatal(err)
	}

	sendinfomsg("Finished stage 1 Sysup boot-strap")
	logtofile("FinishedSysUp Stage 1\n-----------------------")

	doupdatefileumnt("")
}

func cleanupbe() {
	cmd := exec.Command("umount", "-f", defines.STAGEDIR+"/dev")
	cmd.Run()
	cmd = exec.Command("umount", "-f", defines.STAGEDIR)
	cmd.Run()
	cmd = exec.Command(defines.BEBIN, "destroy", "-F", defines.BESTAGE)
	cmd.Run()
}

func createnewbe() {
	// Start creating the new BE and mount it for package ops
	logtofile("Creating new boot-environment")
	sendinfomsg("Creating new Boot-Environment")
	cmd := exec.Command(defines.BEBIN, "create", defines.BESTAGE)
	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
	cmd = exec.Command(
		defines.BEBIN, "mount", defines.BESTAGE, defines.STAGEDIR,
	)
	err = cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
	cmd = exec.Command(
		"mount", "-t", "devfs", "devfs", defines.STAGEDIR+"/dev",
	)
	err = cmd.Run()
	if err != nil {
		log.Fatal(err)
	}

	// Create the directory for the CacheDir
	cmd = exec.Command(
		"mkdir", "-p", defines.STAGEDIR+defines.CacheDir,
	)
	err = cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
	// Mount the CacheDir inside the BE
	cmd = exec.Command(
		"mount", "-t", "nullfs", defines.CacheDir, defines.STAGEDIR+
			defines.CacheDir,
	)
	err = cmd.Run()
	if err != nil {
		log.Fatal(err)
	}

	cmd = exec.Command("rm", "-rf", defines.STAGEDIR+"/var/db/pkg")
	err = cmd.Run()
	if err != nil {
		logtofile("Failed cleanup of: " + defines.STAGEDIR + "/var/db/pkg")
		log.Fatal(err)
	}

	// On FreeNAS /etc/pkg is a nullfs memory system, and we want to catch
	// if any local changes have been made here which aren't yet on the new BE
	cmd = exec.Command("rm", "-rf", defines.STAGEDIR+"/etc/pkg")
	err = cmd.Run()
	if err != nil {
		logtofile("Failed cleanup of: " + defines.STAGEDIR + "/etc/pkg")
		log.Fatal(err)
	}
	cmd = exec.Command("cp", "-r", "/etc/pkg", defines.STAGEDIR+"/etc/pkg")
	err = cmd.Run()
	if err != nil {
		logtofile("Failed copy of: /etc/pkg " + defines.STAGEDIR + "/etc/pkg")
		log.Fatal(err)
	}

	// Copy over the existing local database
	srcDir := defines.PkgDb
	destDir := defines.STAGEDIR + "/var/db/pkg"
	cpCmd := exec.Command("cp", "-r", srcDir, destDir)
	err = cpCmd.Run()
	if err != nil {
		logtofile(
			"Failed copy of: " + defines.PkgDb + " -> " +
				defines.STAGEDIR + "/var/db/pkg",
		)
		log.Fatal(err)
	}

	var reposdir string
	if defines.UpdateFileFlag != "" {
		reposdir = mkreposfile(defines.STAGEDIR, "/var/db/pkg")
	}

	// Update the config file
	fdata := `PKG_CACHEDIR: ` + defines.CacheDir + `
IGNORE_OSVERSION: YES` + `
` + reposdir + `
` + defines.AbiOverride
	ioutil.WriteFile(defines.STAGEDIR+defines.PkgConf, []byte(fdata), 0644)
	logtofile("Done creating new boot-environment")
}

func sanitize_zfs() {

	// If we have a base system ZFS, we need to check if the port needs removing
	_, err := os.Stat(defines.STAGEDIR + "/boot/modules/zfs.ko")
	if err != nil {
		cleanup_zol_port()
	}
}

func cleanup_zol_port() {

	sendinfomsg("Cleaning ZFS Port")
	logtofile("ZFS Port cleanup stage 1\n-----------------------")

	// Update the sysutils/zol port
	cmd := exec.Command(
		defines.PKGBIN, "-c", defines.STAGEDIR, "-C", defines.PkgConf,
		"delete", "-U", "-y", "sysutils/zol-kmod",
	)
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
		errbuf, _ := ioutil.ReadAll(stderr)
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
	cmd := exec.Command(
		defines.PKGBIN, "-c", defines.STAGEDIR, "-C", defines.PkgConf,
		"rquery", "-U", "%v", "os/userland",
	)
	err := cmd.Run()
	if err != nil {
		return
	}

	// We have os/userland remote, lets see if we are using it already locally
	cmd = exec.Command(defines.PKGBIN, "query", "%v", "os/userland")
	err = cmd.Run()
	if err == nil {
		return
	}

	output, cmderr := exec.Command(
		"tar", "czf", defines.STAGEDIR+"/.etcbackup.tgz", "-C",
		defines.STAGEDIR+"/etc", ".",
	).Output()
	if cmderr != nil {
		sendinfomsg(string(output))
		sendfatalmsg("Failed saving /etc configuration")
	}
	// Make sure pkg is fetched
	sendinfomsg("Fetching new base")
	logtofile("Fetching new base")
	cmd = exec.Command(
		defines.PKGBIN, "-C", defines.PkgConf, "fetch", "-U", "-d",
		"-y", "os/userland", "os/kernel",
	)
	err = cmd.Run()
	if err != nil {
		sendfatalmsg("Failed fetching new base")
	}

	// Get list of packages we need to nuke
	shellcmd := defines.PKGBIN +
		" query '%o %n-%v' | grep '^base ' | awk '{print $2}'"
	output, cmderr = exec.Command("/bin/sh", "-c", shellcmd).Output()
	if cmderr != nil {
		sendfatalmsg("Failed getting base package list")
	}

	basepkgs := strings.TrimSpace(string(output))
	barr := strings.Split(basepkgs, "\n")
	for i, _ := range barr {
		// Unset vital flag
		sendinfomsg("Removing: " + barr[i])
		logtofile("Removing: " + barr[i])
		cmd := exec.Command(
			defines.PKGBIN, "-c", defines.STAGEDIR, "-C", defines.PkgConf,
			"set", "-v", "0", barr[i],
		)
		err := cmd.Run()
		if err != nil {
			log.Fatal("Failed unsetting vital")
		}
		// Remove the package now
		cmd = exec.Command(defines.PKGBIN, "-c", defines.STAGEDIR,
			"-C", defines.PkgConf, "delete", "-y", "-f", "-g", barr[i],
		)
		remout, err := cmd.CombinedOutput()
		if err != nil {
			sendinfomsg(string(remout))
			sendfatalmsg("Failed removing " + barr[i])
		}
		if strings.Contains(barr[i], "-runtime-1") {
			// If this was the runtime package, need to re-install userland right away
			sendinfomsg("Boot-strapping userland")
			pkgcmd := exec.Command(
				defines.PKGBIN, "-r", defines.STAGEDIR, "-C", defines.PkgConf,
				"install", "-U", "-f", "-y", "os/userland",
			)
			fullout, err := pkgcmd.CombinedOutput()
			if err != nil {
				sendinfomsg(string(fullout))
				sendfatalmsg("Failed boot-strapping userland")
			}
		}
	}

	// Load new userland / kernel and friends
	pkgcmd := exec.Command(
		defines.PKGBIN, "-c", defines.STAGEDIR, "-C", defines.PkgConf,
		"install", "-U", "-y", "os/userland", "os/kernel",
	)
	fullout, err := pkgcmd.CombinedOutput()
	sendinfomsg(string(fullout))
	logtofile(string(fullout))

	// Ensure pkg is boot-strapped again
	pkgcmd = exec.Command(defines.PKGBIN, "-c", defines.STAGEDIR, "-C",
		defines.PkgConf, "install", "-U", "-y", "ports-mgmt/pkg",
	)
	fullout, err = pkgcmd.CombinedOutput()
	sendinfomsg(string(fullout))
	logtofile(string(fullout))

	// Copy back the /etc changes
	output, cmderr = exec.Command(
		"tar", "xf", defines.STAGEDIR+"/.etcbackup.tgz", "-C",
		defines.STAGEDIR+"/etc",
	).CombinedOutput()
	if cmderr != nil {
		sendinfomsg(string(output))
		sendinfomsg("WARNING: Tar error while updating /etc configuration")
	}
	exec.Command("rm", defines.STAGEDIR+"/.etcbackup.tgz").Run()

	// Make sure sysup is marked as installed
	pkgcmd = exec.Command(defines.PKGBIN, "-c", defines.STAGEDIR, "-C",
		defines.PkgConf, "set", "-y", "-A", "00", "sysutils/sysup",
	)
	fullout, err = pkgcmd.CombinedOutput()
	sendinfomsg(string(fullout))
	logtofile(string(fullout))
}

func updateincremental(force bool) {
	sendinfomsg("Starting package update")
	logtofile("PackageUpdate\n-----------------------")

	// Check if we are moving from legacy pkg base to ports-base
	checkbaseswitch()

	pkgcmd := exec.Command(
		defines.PKGBIN, "-c", defines.STAGEDIR, "-C", defines.PkgConf,
		"upgrade", "-U", "-y", "-f", "ports-mgmt/pkg",
	)
	fullout, err := pkgcmd.CombinedOutput()
	sendinfomsg(string(fullout))
	logtofile(string(fullout))

	// Setup our main update process
	cmd := exec.Command(defines.PKGBIN)
	cmd.Args = append(cmd.Args, "-c")
	cmd.Args = append(cmd.Args, defines.STAGEDIR)
	cmd.Args = append(cmd.Args, "-C")
	cmd.Args = append(cmd.Args, defines.PkgConf)
	cmd.Args = append(cmd.Args, "upgrade")
	cmd.Args = append(cmd.Args, "-U")
	cmd.Args = append(cmd.Args, "-I")
	cmd.Args = append(cmd.Args, "-y")

	// Reinstall everything?
	if force {
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
	critpkg := []string{"ports-mgmt/pkg", "os/userland", "os/kernel"}
	for i, _ := range critpkg {
		pkgcmd = exec.Command(
			defines.PKGBIN, "-c", defines.STAGEDIR, "-C", defines.PkgConf,
			"set", "-y", "-A", "00", critpkg[i],
		)
		fullout, err = pkgcmd.CombinedOutput()
		sendinfomsg(string(fullout))
		logtofile(string(fullout))
	}

	// Cleanup orphans
	pkgcmd = exec.Command(
		defines.PKGBIN, "-c", defines.STAGEDIR, "-C", defines.PkgConf,
		"autoremove", "-y",
	)
	fullout, err = pkgcmd.CombinedOutput()
	sendinfomsg(string(fullout))
	logtofile(string(fullout))

}

func startupgrade() {

	cleanupbe()

	createnewbe()

	// If we are using standalone update need to nullfs mount the pkgs
	doupdatefilemnt()

	updateincremental(defines.FullUpdateFlag)

	// Check if we need to do any ZFS automagic
	sanitize_zfs()

	// Update the boot-loader
	updateloader(defines.STAGEDIR)

	// Cleanup nullfs mount
	doupdatefileumnt(defines.STAGEDIR)

	// Unmount the devfs point
	cmd := exec.Command("umount", "-f", defines.STAGEDIR+"/dev")
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

	BENAME := strconv.Itoa(year) +
		"-" +
		strconv.Itoa(month) +
		"-" +
		strconv.Itoa(day) +
		"-" +
		strconv.Itoa(hour) +
		"-" +
		strconv.Itoa(min) +
		"-" +
		strconv.Itoa(sec)

	if defines.BeNameFlag != "" {
		BENAME = defines.BeNameFlag
	}

	// Start by unmounting BE
	cmd := exec.Command("beadm", "umount", "-f", defines.BESTAGE)
	err := cmd.Run()
	if err != nil {
		logtofile("Failed beadm umount -f")
		log.Fatal(err)
	}

	// Now rename BE
	cmd = exec.Command("beadm", "rename", defines.BESTAGE, BENAME)
	err = cmd.Run()
	if err != nil {
		logtofile("Failed beadm rename")
		log.Fatal(err)
	}

	// Lastly setup a boot of this new BE
	cmd = exec.Command("beadm", "activate", BENAME)
	err = cmd.Run()
	if err != nil {
		logtofile("Failed beadm activate")
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

	src := defines.LogFile
	dest := "/var/log/updatego.failed"
	cpCmd := exec.Command("cp", src, dest)
	cpCmd.Run()

	sendfatalmsg("Aborting")
}

func startfetch() error {

	cmd := exec.Command(
		defines.PKGBIN, "-C", defines.PkgConf, "upgrade", "-F", "-y", "-U",
	)

	// Are we doing a full update?
	if defines.FullUpdateFlag {
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
		errbuf, _ := ioutil.ReadAll(stderr)
		errarr := strings.Split(string(errbuf), "\n")
		for i, _ := range errarr {
			logtofile(errarr[i])
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
	if oerr != nil {
		log.Fatal(oerr)
	}
	REMOTEVER, err := getremoteosver()
	if err != nil {
		log.Fatal(err)
	}

	OSVER := fmt.Sprint(OSINT)
	logtofile("OS Version: " + OSVER + " -> " + REMOTEVER)
	if OSVER != REMOTEVER {
		sendinfomsg("Remote ABI change detected: " + OSVER + " -> " + REMOTEVER)
		logtofile("Remote ABI change detected: " + OSVER + " -> " + REMOTEVER)
		return true
	}
	return false
}

func updateloader(stagedir string) {
	logtofile("Updating Bootloader\n-------------------")
	sendinfomsg("Updating Bootloader")
	disks := getzpooldisks()
	for i, _ := range disks {
		if isuefi(disks[i]) {
			logtofile("Updating EFI boot-loader on: " + disks[i])
			sendinfomsg("Updating EFI boot-loader on: " + disks[i])
			if !updateuefi(disks[i], stagedir) {
				sendfatalmsg("Updating boot-loader failed!")
			}
		} else {
			logtofile("Updating GPT boot-loader on: " + disks[i])
			sendinfomsg("Updating GPT boot-loader on: " + disks[i])
			if !updategpt(disks[i], stagedir) {
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
		if strings.Contains(line, " efi ") {
			linearray := strings.Fields(line)
			if len(linearray) < 3 {
				sendinfomsg("ERROR: Unable to locate EFI partition")
				logtofile("Unable to locate EFI partition..." + string(line))
				return false
			}
			part := linearray[2]

			// Mount the UEFI partition
			bcmd := exec.Command("mount", "-t", "msdosfs", "/dev/"+disk+"p"+part, "/boot/efi")
			berr := bcmd.Run()
			if berr != nil {
				sendinfomsg("ERROR: Unable to mount EFI partition")
				logtofile("Unable to mount EFI partition: " + part)
				return false
			}

			// Copy the new UEFI file over
			var tgt string
			if _, err := os.Stat("/boot/efi/efi/boot/bootx64-trueos.efi"); os.IsNotExist(err) {
				tgt = "/boot/efi/efi/boot/bootx64-trueos.efi"
			} else {
				tgt = "/boot/efi/efi/boot/bootx64.efi"
			}
			cmd := exec.Command("cp", stagedir+"/boot/loader.efi", tgt)
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
		logtofile("ERROR: Failed gpart show")
		return false
	}
	if err := cmd.Start(); err != nil {
		sendinfomsg("ERROR: Failed starting gpart show")
		logtofile("ERROR: Failed starting gpart show")
		return false
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and look for specific boot partition
	for buff.Scan() {
		line := strings.TrimSpace(buff.Text())
		if strings.Contains(line, " freebsd-boot ") {
			linearray := strings.Fields(line)
			if len(linearray) < 3 {
				sendinfomsg("ERROR: Failed to locate GPT boot partition...")
				logtofile("Unable to locate GPT boot partition..." + string(line))
				return false
			}
			part := linearray[2]
			bcmd := exec.Command("gpart", "bootcode", "-b", stagedir+"/boot/pmbr", "-p", stagedir+"/boot/gptzfsboot", "-i", part, disk)
			berr := bcmd.Run()
			if berr != nil {
				sendinfomsg("Failed gpart bootcode -b " + stagedir + "/boot/pmbr -p " + stagedir + "/boot/gptzfsboot -i " + part + " " + disk)
				logtofile("Failed gpart bootcode -b " + stagedir + "/boot/pmbr -p " + stagedir + "/boot/gptzfsboot -i " + part + " " + disk)
				return false
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
		if strings.Contains(line, " efi ") {
			return true
		}
		if strings.Contains(line, "freebsd-boot") {
			return false
		}
	}
	return false
}

func getberoot() string {
	// Get the current BE root
	shellcmd := "mount | awk '/ \\/ / {print $1}'"
	output, cmderr := exec.Command("/bin/sh", "-c", shellcmd).Output()
	if cmderr != nil {
		log.Fatal("Failed determining ZFS root")
	}
	currentbe := output
	linearray := strings.Split(string(currentbe), "/")
	if len(linearray) < 2 {
		log.Fatal("Invalid beroot: " + string(currentbe))
	}
	beroot := linearray[0] + "/" + linearray[1]
	return beroot
}

func getcurrentbe() string {
	// Get the current BE root
	shellcmd := "mount | awk '/ \\/ / {print $1}'"
	output, cmderr := exec.Command("/bin/sh", "-c", shellcmd).Output()
	if cmderr != nil {
		log.Fatal("Failed determining ZFS root")
	}
	currentbe := output
	linearray := strings.Split(string(currentbe), "/")
	if len(linearray) < 2 {
		log.Fatal("Invalid beroot: " + string(currentbe))
	}
	be := linearray[2]
	return be
}

func getzfspool() string {
	// Get the current BE root
	shellcmd := "mount | awk '/ \\/ / {print $1}'"
	output, cmderr := exec.Command("/bin/sh", "-c", shellcmd).Output()
	if cmderr != nil {
		log.Fatal("Failed determining ZFS root")
	}
	currentbe := output
	linearray := strings.Split(string(currentbe), "/")
	if len(linearray) < 2 {
		log.Fatal("Invalid beroot: " + string(currentbe))
	}
	return linearray[0]
}

func getzpooldisks() []string {
	var diskarr []string
	zpool := getzfspool()
	kernout, kerr := syscall.Sysctl("kern.disks")
	if kerr != nil {
		log.Fatal(kerr)
	}
	kerndisks := strings.Split(string(kernout), " ")
	for i, _ := range kerndisks {
		// Yes, CD's show up in this output..
		if strings.Index(kerndisks[i], "cd") == 0 {
			continue
		}
		// Get a list of uuids for the partitions on this disk
		duuids := getdiskuuids(kerndisks[i])

		// Validate this disk is in the default zpool
		if !diskisinpool(kerndisks[i], duuids, zpool) {
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
		if strings.Contains(line, " "+disk+" ") {
			return true
		}
		if strings.Contains(line, " "+disk+"p") {
			return true
		}
		for i, _ := range uuids {
			if strings.Contains(line, " gptid/"+uuids[i]) {
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
