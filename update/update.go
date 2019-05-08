package update

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/trueos/sysup/defines"
	"github.com/trueos/sysup/logger"
	"github.com/trueos/sysup/pkg"
	"github.com/trueos/sysup/utils"
	"github.com/trueos/sysup/ws"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func DoUpdate(message []byte) {
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
	defines.FetchOnlyFlag = s.Fetchonly
	//log.Println("benameflag: " + benameflag)
	//log.Println("updatefile: " + updatefileflag)

	// Update any variable locations to use cachedirflag
	defines.SetLocs()

	// Start a fresh log file
	logger.RotateLog()

	// Setup the pkg config directory
	logger.LogToFile("Setting up pkg database")
	pkg.PreparePkgConfig("")

	// Update the package database
	logger.LogToFile("Updating package repo database")
	pkg.UpdatePkgDb("")

	// Check that updates are available
	logger.LogToFile("Checking for updates")
	details, haveupdates, uerr := pkg.UpdateDryRun(false)
	if uerr != nil {
		return
	}
	if !haveupdates && !defines.FullUpdateFlag {
		ws.SendMsg("No updates to install!", "fatal")
		return
	}

	// Check host OS version
	logger.LogToFile("Checking OS version")
	if pkg.HaveOsVerChange() {
		defines.FullUpdateFlag = true
	}

	// Check if we are moving from pre-flavor pkg base to flavors
	checkFlavorSwitch()

	// Start downloading our files if we aren't doing stand-alone upgrade
	if defines.UpdateFileFlag == "" {
		logger.LogToFile("Fetching file updates")
		startfetch()
	}

	// User does not want to apply updates
	if defines.FetchOnlyFlag {
		return
	}

	// If we have a sysup package we intercept here, do boot-strap and
	// Then restart the update with the fresh binary on a new port
	//
	// Skip if the disablebsflag is set
	if details.SysUp && !defines.DisableBsFlag {
		logger.LogToFile("Performing bootstrap")
		dosysupbootstrap()
		err := dopassthroughupdate()

		if err != nil {
			log.Fatalln(err)
		}
		return
	}

	// Start the upgrade
	startupgrade()
}

// This is called after a sysup boot-strap has taken place
//
// We will restart the sysup daemon on a new port and continue
// with the same update as previously requested
func dopassthroughupdate() error {
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

	// Start the newly updated sysup binary, passing along our previous flags
	//upflags := fuflag + " " + upflag + " " + beflag + " " + ukeyflag
	cmd := exec.Command("sysup", "-port=0", "-update")

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

	bsMsg := "Running bootstrap with flags: " + strings.Join(cmd.Args, " ")
	logger.LogToFile(bsMsg)
	ws.SendMsg(bsMsg)

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

	// Iterate over buff and log content
	for buff.Scan() {
		line := buff.Text()
		ws.SendMsg(line)
	}
	// If we get a non-0 back, report the full error
	if err := cmd.Wait(); err != nil {
		errbuf, _ := ioutil.ReadAll(stderr)
		errarr := strings.Split(string(errbuf), "\n")
		for i, _ := range errarr {
			logger.LogToFile(errarr[i])
			ws.SendMsg(errarr[i])
		}
		ws.SendMsg("Failed sysup bootstrap!", "fatal")
		return err
	}

	// Let our local clients know they can finish up
	ws.SendMsg("", "shutdown")

	return nil
}

func doupdatefileumnt(prefix string) {
	if defines.UpdateFileFlag == "" {
		return
	}

	logger.LogToFile("Unmount nullfs")
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

	logger.LogToFile("Mounting nullfs")
	cmd := exec.Command(
		"mount_nullfs", defines.ImgMnt,
		defines.STAGEDIR+defines.ImgMnt,
	)
	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
	logger.LogToFile("NullFS mounted at: " + defines.ImgMnt)
}

// When we have a new version of sysup to upgrade to, we perform
// that update first, and then continue with the regular update
func dosysupbootstrap() {

	// Start by updating the sysup PKG
	ws.SendMsg("Starting Sysup boot-strap")
	logger.LogToFile("SysUp Stage 1\n-----------------------")

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
		ws.SendMsg(line)
		logger.LogToFile(line)
	}
	// Pkg returns 0 on sucess
	if err := cmd.Wait(); err != nil {
		ws.SendMsg("Failed sysup update!", "fatal")
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

	ws.SendMsg("Finished stage 1 Sysup boot-strap")
	logger.LogToFile("FinishedSysUp Stage 1\n-----------------------")

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
	logger.LogToFile("Creating new boot-environment")
	ws.SendMsg("Creating new Boot-Environment")
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
		logger.LogToFile(
			"Failed cleanup of: " + defines.STAGEDIR + "/var/db/pkg",
		)
		log.Fatal(err)
	}

	// On FreeNAS /etc/pkg is a nullfs memory system, and we want to catch
	// if any local changes have been made here which aren't yet on the new BE
	cmd = exec.Command("rm", "-rf", defines.STAGEDIR+"/etc/pkg")
	err = cmd.Run()
	if err != nil {
		logger.LogToFile("Failed cleanup of: " + defines.STAGEDIR + "/etc/pkg")
		log.Fatal(err)
	}
	cmd = exec.Command("cp", "-r", "/etc/pkg", defines.STAGEDIR+"/etc/pkg")
	err = cmd.Run()
	if err != nil {
		logger.LogToFile(
			"Failed copy of: /etc/pkg " + defines.STAGEDIR + "/etc/pkg",
		)
		log.Fatal(err)
	}

	// Copy over the existing local database
	srcDir := defines.PkgDb
	destDir := defines.STAGEDIR + "/var/db/pkg"
	cpCmd := exec.Command("cp", "-r", srcDir, destDir)
	err = cpCmd.Run()
	if err != nil {
		logger.LogToFile(
			"Failed copy of: " + defines.PkgDb + " -> " +
				defines.STAGEDIR + "/var/db/pkg",
		)
		log.Fatal(err)
	}

	var reposdir string
	if defines.UpdateFileFlag != "" {
		reposdir = pkg.MkReposFile(defines.STAGEDIR, "/var/db/pkg")
	}

	// Update the config file
	fdata := `PKG_CACHEDIR: ` + defines.CacheDir + `
IGNORE_OSVERSION: YES` + `
` + reposdir + `
` + defines.AbiOverride
	ioutil.WriteFile(defines.STAGEDIR+defines.PkgConf, []byte(fdata), 0644)
	logger.LogToFile("Done creating new boot-environment")
}

func sanitize_zfs() {

	// If we have a base system ZFS, we need to check if the port needs
	// removing
	_, err := os.Stat(defines.STAGEDIR + "/boot/modules/zfs.ko")
	if err != nil {
		cleanup_zol_port()
	}
}

func cleanup_zol_port() {

	ws.SendMsg("Cleaning ZFS Port")
	logger.LogToFile("ZFS Port cleanup stage 1\n-----------------------")

	// Update the sysutils/zol port
	cmd := exec.Command(
		defines.PKGBIN, "-c", defines.STAGEDIR, "-C", defines.PkgConf,
		"delete", "-U", "-y", "sysutils/zol-kmod",
	)
	logger.LogToFile(
		"Cleaning up ZFS port with: " + strings.Join(cmd.Args, " "),
	)
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

	// Iterate over buff and log content
	for buff.Scan() {
		line := buff.Text()
		ws.SendMsg(line)
		logger.LogToFile(line)
	}
	// Pkg returns 0 on sucess
	if err := cmd.Wait(); err != nil {
		errbuf, _ := ioutil.ReadAll(stderr)
		errarr := strings.Split(string(errbuf), "\n")
		for i, _ := range errarr {
			ws.SendMsg(errarr[i])
			logger.LogToFile(errarr[i])
		}
	}
	ws.SendMsg("Finished stage 1 ZFS update")
	logger.LogToFile(
		"Finished ZFS port cleanup stage 1\n-----------------------",
	)
}

func checkFlavorSwitch() {
	// Does the new pkg repo have os-generic-userland flavorized package
	cmd := exec.Command(
		defines.PKGBIN, "-C", defines.PkgConf,
		"rquery", "-U", "%v", "os-generic-userland",
	)
	if err := cmd.Run(); err != nil {
		return
	}

	// We have flavorized package, lets see if we are still using the
	// old non-flavor version still
	cmd = exec.Command(defines.PKGBIN, "query", "%v", "userland")
	if err := cmd.Run(); err != nil {
		// We are not using the old, we can safely return now
		return
	}

	var pkgSlice []string
	pkgArgs := []string{
		defines.PKGBIN, "-C", defines.PkgConf, "set", "--change-name",
	}

	// Update the old style base packages to their flavor versions
	if _, err := os.Stat(
		"/boot/kernel/zfs.ko",
	); os.IsNotExist(err) {
		// Switch to a ZOL base flavor
		ws.SendMsg("Switching to ZOL base flavor")
		pkgSlice = append(
			pkgSlice,
			"userland:os-zol-userland",
			"userland-base:os-zol-userland-base",
			"userland-debug:os-zol-userland-debug",
			"userland-docs:os-zol-userland-docs",
			"userland-lib32:os-zol-userland-lib32",
			"userland-tests:os-zol-userland-tests",
			"kernel:os-zol-kernel",
			"kernel-debug:os-zol-kernel-debug",
			"kernel-debug-symbols:os-zol-kernel-debug-symbols",
			"kernel-symbols:os-zol-kernel-symbols",
			"buildkernel:os-zol-buildkernel",
			"buildworld:os-zol-buildworld",
		)

	} else {
		// Switch to a GENERIC base flavor
		ws.SendMsg("Switching to GENERIC base flavor")
		pkgSlice = append(
			pkgSlice,
			"userland:os-generic-userland",
			"userland-base:os-generic-userland-base",
			"userland-debug:os-generic-userland-debug",
			"userland-docs:os-generic-userland-docs",
			"userland-lib32:os-generic-userland-lib32",
			"userland-tests:os-generic-userland-tests",
			"kernel:os-generic-kernel",
			"kernel-debug:os-generic-kernel-debug",
			"kernel-debug-symbols:os-generic-kernel-debug-symbols",
			"kernel-symbols:os-generic-kernel-symbols",
			"buildkernel:os-generic-buildkernel",
			"buildworld:os-generic-buildworld",
		)
	}

	for _, pkg := range pkgSlice {
		args := append(pkgArgs, pkg, "-y")

		if out, err := exec.Command(
			args[0], args[1:]...,
		).CombinedOutput(); err != nil {
			ws.SendMsg(
				pkg+" failed to install! Error:\n"+string(out), "fatal",
			)
			log.Fatal(out)
		}
	}
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
		ws.SendMsg(string(output))
		ws.SendMsg("Failed saving /etc configuration", "fatal")
	}
	// Make sure pkg is fetched
	ws.SendMsg("Fetching new base")
	logger.LogToFile("Fetching new base")
	cmd = exec.Command(
		defines.PKGBIN, "-C", defines.PkgConf, "fetch", "-U", "-d",
		"-y", "os/userland", "os/kernel",
	)
	err = cmd.Run()
	if err != nil {
		ws.SendMsg("Failed fetching new base", "fatal")
	}

	// Get list of packages we need to nuke
	shellcmd := defines.PKGBIN +
		" query '%o %n-%v' | grep '^base ' | awk '{print $2}'"
	output, cmderr = exec.Command("/bin/sh", "-c", shellcmd).Output()
	if cmderr != nil {
		ws.SendMsg("Failed getting base package list", "fatal")
	}

	basepkgs := strings.TrimSpace(string(output))
	barr := strings.Split(basepkgs, "\n")
	for i, _ := range barr {
		// Unset vital flag
		ws.SendMsg("Removing: " + barr[i])
		logger.LogToFile("Removing: " + barr[i])
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
			ws.SendMsg(string(remout))
			ws.SendMsg("Failed removing "+barr[i], "fatal")
		}
		if strings.Contains(barr[i], "-runtime-1") {
			// If this was the runtime package, need to re-install userland
			// right away
			ws.SendMsg("Boot-strapping userland")
			pkgcmd := exec.Command(
				defines.PKGBIN, "-r", defines.STAGEDIR, "-C", defines.PkgConf,
				"install", "-U", "-f", "-y", "os/userland",
			)
			fullout, err := pkgcmd.CombinedOutput()
			if err != nil {
				ws.SendMsg(string(fullout))
				ws.SendMsg("Failed boot-strapping userland", "fatal")
			}
		}
	}

	// Load new userland / kernel and friends
	pkgcmd := exec.Command(
		defines.PKGBIN, "-c", defines.STAGEDIR, "-C", defines.PkgConf,
		"install", "-U", "-y", "os/userland", "os/kernel",
	)
	fullout, err := pkgcmd.CombinedOutput()
	ws.SendMsg(string(fullout))
	logger.LogToFile(string(fullout))

	// Ensure pkg is boot-strapped again
	pkgcmd = exec.Command(defines.PKGBIN, "-c", defines.STAGEDIR, "-C",
		defines.PkgConf, "install", "-U", "-y", "ports-mgmt/pkg",
	)
	fullout, err = pkgcmd.CombinedOutput()
	ws.SendMsg(string(fullout))
	logger.LogToFile(string(fullout))

	// Copy back the /etc changes
	output, cmderr = exec.Command(
		"tar", "xf", defines.STAGEDIR+"/.etcbackup.tgz", "-C",
		defines.STAGEDIR+"/etc",
	).CombinedOutput()
	if cmderr != nil {
		ws.SendMsg(string(output))
		ws.SendMsg("WARNING: Tar error while updating /etc configuration")
	}
	exec.Command("rm", defines.STAGEDIR+"/.etcbackup.tgz").Run()

	// Make sure sysup is marked as installed
	pkgcmd = exec.Command(defines.PKGBIN, "-c", defines.STAGEDIR, "-C",
		defines.PkgConf, "set", "-y", "-A", "00", "sysutils/sysup",
	)
	fullout, err = pkgcmd.CombinedOutput()
	ws.SendMsg(string(fullout))
	logger.LogToFile(string(fullout))
}

func updateincremental(force bool) error {
	var stdoutBuf, stderrBuf bytes.Buffer
	var errStdout, errStderr error

	ws.SendMsg("Starting package update")
	logger.LogToFile("PackageUpdate\n-----------------------")

	// Check if we are moving from legacy pkg base to ports-base
	checkbaseswitch()

	// Make sure the BE has a valid resolv.conf
	resolv_dest := defines.STAGEDIR + "/etc/resolv.conf"
	_, err := utils.Copyfile("/etc/resolv.conf", resolv_dest)
	if err != nil {
		err_string := fmt.Sprintf(
			"Copying /etc/resolv.conf failed: %s\n", err,
		)
		pkg.DestroyMdDev()
		logger.LogToFile(err_string)
		ws.SendMsg(err_string, "fatal")

		return errors.New(err_string)
	}

	pkgcmd := exec.Command(
		defines.PKGBIN, "-c", defines.STAGEDIR, "-C", defines.PkgConf,
		"upgrade", "-U", "-y", "-f", "ports-mgmt/pkg",
	)
	fullout, err := pkgcmd.CombinedOutput()
	if err != nil {
		lastMessage := bytes.Split(fullout, []byte{'\n'})
		// -1 is a newline
		err_string := fmt.Sprintf(
			"Upgrading pkg failed: %s\n", lastMessage[len(lastMessage)-2],
		)
		pkg.DestroyMdDev()
		logger.LogToFile(err_string)
		ws.SendMsg(err_string, "fatal")

		return errors.New(err_string)
	}

	ws.SendMsg(string(fullout))
	logger.LogToFile(string(fullout))

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

	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()
	stdout := io.MultiWriter(os.Stdout, &stdoutBuf)
	stderr := io.MultiWriter(os.Stderr, &stderrBuf)

	// Reinstall everything?
	if force {
		cmd.Args = append(cmd.Args, "-f")
	}
	logger.LogToFile("Starting upgrade with: " + strings.Join(cmd.Args, " "))
	if err := cmd.Start(); err != nil {
		pkg.DestroyMdDev()
		err_string := fmt.Sprintf("Starting pkg upgrade failed: %s\n", err)
		logger.LogToFile(err_string)
		ws.SendMsg(err_string, "fatal")

		return errors.New(err_string)
	}

	// We want to make sure we aren't blocking stdout
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		_, errStdout = io.Copy(stdout, stdoutPipe)
		wg.Done()
	}()

	_, errStderr = io.Copy(stderr, stderrPipe)
	wg.Wait()

	// Pkg returns 0 on success
	if err := cmd.Wait(); err != nil {
		pkg.DestroyMdDev()
		err_string := fmt.Sprintf(
			"Failed pkg upgrade:\n%s\n", string(stderrBuf.Bytes()),
		)
		logger.LogToFile(err_string)
		ws.SendMsg(err_string, "fatal")

		return errors.New(err_string)
	}

	if errStdout != nil || errStderr != nil {
		pkg.DestroyMdDev()
		err_string := "Failed to capture stdout or stderr\n"
		logger.LogToFile(err_string)
		ws.SendMsg(err_string, "fatal")

		return errors.New(err_string)
	}

	bufStdout := strings.NewReader(string(stdoutBuf.Bytes()))
	buff := bufio.NewScanner(bufStdout)

	// Iterate over buff and log content
	for buff.Scan() {
		line := buff.Text()
		ws.SendMsg(line)
		logger.LogToFile("pkg: " + line)
	}

	ws.SendMsg("Finished stage package update")
	logger.LogToFile("FinishedPackageUpdate\n-----------------------")

	// Mark essential pkgs
	critpkg := []string{"ports-mgmt/pkg", "os/userland", "os/kernel"}
	for i, _ := range critpkg {
		pkgcmd = exec.Command(
			defines.PKGBIN, "-c", defines.STAGEDIR, "-C", defines.PkgConf,
			"set", "-y", "-A", "00", critpkg[i],
		)
		fullout, _ = pkgcmd.CombinedOutput()
		ws.SendMsg(string(fullout))
		logger.LogToFile(string(fullout))
	}

	// Cleanup orphans
	pkgcmd = exec.Command(
		defines.PKGBIN, "-c", defines.STAGEDIR, "-C", defines.PkgConf,
		"autoremove", "-y",
	)
	// err isn't used
	fullout, _ = pkgcmd.CombinedOutput()
	ws.SendMsg(string(fullout))
	logger.LogToFile(string(fullout))

	return nil
}

func startupgrade() {

	cleanupbe()

	createnewbe()

	// If we are using standalone update need to nullfs mount the pkgs
	doupdatefilemnt()

	if err := updateincremental(defines.FullUpdateFlag); err != nil {
		return
	}

	// Check if we need to do any ZFS automagic
	sanitize_zfs()

	// Update the bootloader
	UpdateLoader(defines.STAGEDIR)

	// Cleanup nullfs mount
	doupdatefileumnt(defines.STAGEDIR)

	// Unmount the devfs point
	cmd := exec.Command("umount", "-f", defines.STAGEDIR+"/dev")
	cmd.Run()

	// Rename to proper BE name
	renamebe()

	// If we are using standalone update, cleanup
	pkg.DestroyMdDev()
	ws.SendMsg(
		"Success! Reboot your system to continue the update process.",
		"shutdown",
	)
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
		logger.LogToFile("Failed beadm umount -f")
		ws.SendMsg("Failed unmounting: "+defines.BESTAGE, "fatal")
		log.Fatal(err)
	}

	// Now mount BE
	cmd = exec.Command("beadm", "mount", defines.BESTAGE, "/var/tmp/"+BENAME)
	err = cmd.Run()

	if err != nil {
		logger.LogToFile("Failed beadm mount")
		ws.SendMsg("Failed mounting: "+defines.BESTAGE, "fatal")
		log.Fatal(err)
	}

	// Now rename BE
	cmd = exec.Command("beadm", "rename", defines.BESTAGE, BENAME)
	err = cmd.Run()
	if err != nil {
		logger.LogToFile("Failed beadm rename")
		ws.SendMsg("Failed renaming: "+BENAME, "fatal")
		log.Fatal(err)
	}

	// beadm requires this to exist
	loaderConf := "/var/tmp/" + BENAME + "/boot/loader.conf"
	cmd = exec.Command("touch", loaderConf)
	err = cmd.Run()
	if err != nil {
		logger.LogToFile("Failed touching " + loaderConf)
		ws.SendMsg("Failed touching: " + loaderConf)
		log.Fatal("Failed touching: " + loaderConf)
	}

	// Lastly setup a boot of this new BE
	cmd = exec.Command("beadm", "activate", BENAME)
	err = cmd.Run()
	if err != nil {
		logger.LogToFile("Failed beadm activate")
		ws.SendMsg("Failed activating: "+BENAME, "fatal")
		log.Fatal("Failed activating: " + BENAME)
	}

}

/*
* Something has gone horribly wrong, lets make a copy of the
* log file and reboot into the old BE for later debugging
 */
func copylogexit(perr error, text string) {

	logger.LogToFile("FAILED Upgrade!!!")
	logger.LogToFile(perr.Error())
	logger.LogToFile(text)
	log.Println(text)

	src := defines.LogFile
	dest := "/var/log/updatego.failed"
	cpCmd := exec.Command("cp", src, dest)
	cpCmd.Run()

	ws.SendMsg("Aborting", "fatal")
}

func startfetch() error {

	cmd := exec.Command(
		defines.PKGBIN, "-C", defines.PkgConf, "upgrade", "-F", "-y", "-U",
	)

	// Are we doing a full update?
	if defines.FullUpdateFlag {
		cmd.Args = append(cmd.Args, "-f")
	}

	ws.SendMsg("Starting package downloads")
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

	// Iterate over buff and log content
	for buff.Scan() {
		line := buff.Text()
		ws.SendMsg(line)
	}
	// If we get a non-0 back, report the full error
	if err := cmd.Wait(); err != nil {
		errbuf, _ := ioutil.ReadAll(stderr)
		errarr := strings.Split(string(errbuf), "\n")
		for i, _ := range errarr {
			logger.LogToFile(errarr[i])
			ws.SendMsg(errarr[i])
		}
		ws.SendMsg("Failed package fetch!", "fatal")
		return err
	}
	ws.SendMsg("Finished package downloads")

	return nil
}

func UpdateLoader(stagedir string) {
	logger.LogToFile("Updating Bootloader\n-------------------")
	ws.SendMsg("Updating Bootloader")
	disks := getzpooldisks()
	for i, _ := range disks {
		if isuefi(disks[i]) {
			logger.LogToFile("Updating EFI bootloader on: " + disks[i])
			ws.SendMsg("Updating EFI bootloader on: " + disks[i])
			if !updateuefi(disks[i], stagedir) {
				ws.SendMsg("Updating bootloader failed!", "fatal")
			}
		} else {
			logger.LogToFile("Updating GPT bootloader on: " + disks[i])
			ws.SendMsg("Updating GPT bootloader on: " + disks[i])
			if !updategpt(disks[i], stagedir) {
				ws.SendMsg("Updating bootloader failed!", "fatal")
			}
		}
	}
}

func updateuefi(disk string, stagedir string) bool {
	derr := os.MkdirAll("/boot/efi", 0755)
	if derr != nil {
		ws.SendMsg("ERROR: Failed mkdir /boot/efi")
		copylogexit(derr, "Failed mkdir /boot/efi")
	}

	cmd := exec.Command("gpart", "show", disk)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		ws.SendMsg("ERROR: Failed gpart show")
		copylogexit(derr, "Failed gpart show")
	}
	if err := cmd.Start(); err != nil {
		ws.SendMsg("ERROR: Failed starting gpart show")
		copylogexit(derr, "Failed starting gpart show")
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and look for specific boot partition
	for buff.Scan() {
		line := strings.TrimSpace(buff.Text())
		if strings.Contains(line, " efi ") {
			linearray := strings.Fields(line)
			if len(linearray) < 3 {
				ws.SendMsg("ERROR: Unable to locate EFI partition")
				logger.LogToFile(
					"Unable to locate EFI partition..." + string(line),
				)
				return false
			}
			part := linearray[2]

			// Mount the UEFI partition
			bcmd := exec.Command(
				"mount", "-t", "msdosfs", "/dev/"+disk+"p"+part, "/boot/efi",
			)
			berr := bcmd.Run()
			if berr != nil {
				ws.SendMsg("ERROR: Unable to mount EFI partition")
				logger.LogToFile("Unable to mount EFI partition: " + part)
				return false
			}

			// Copy the new UEFI file over
			var tgt string
			if _, err := os.Stat(
				"/boot/efi/efi/boot/bootx64-trueos.efi",
			); os.IsNotExist(err) {
				tgt = "/boot/efi/efi/boot/bootx64-trueos.efi"
			} else {
				tgt = "/boot/efi/efi/boot/bootx64.efi"
			}
			cmd := exec.Command("cp", stagedir+"/boot/loader.efi", tgt)
			cerr := cmd.Run()
			if cerr != nil {
				ws.SendMsg("ERROR: Unable to copy efi file " + tgt)
				logger.LogToFile("Unable to copy efi file: " + tgt)
				return false
			}

			// Unmount and cleanup
			bcmd = exec.Command("umount", "-f", "/boot/efi")
			berr = bcmd.Run()
			if berr != nil {
				ws.SendMsg("ERROR: Unable to umount EFI pratition " + part)
				logger.LogToFile("Unable to umount EFI partition: " + part)
				return false
			}

			return true
		}
	}
	ws.SendMsg("ERROR: Unable to locate EFI partition on: " + string(disk))
	logger.LogToFile("Unable to locate EFI partition on: " + string(disk))
	return false
}

func updategpt(disk string, stagedir string) bool {
	cmd := exec.Command("gpart", "show", disk)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		ws.SendMsg("ERROR: Failed gpart show")
		logger.LogToFile("ERROR: Failed gpart show")
		return false
	}
	if err := cmd.Start(); err != nil {
		ws.SendMsg("ERROR: Failed starting gpart show")
		logger.LogToFile("ERROR: Failed starting gpart show")
		return false
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and look for specific boot partition
	for buff.Scan() {
		line := strings.TrimSpace(buff.Text())
		if strings.Contains(line, " freebsd-boot ") {
			linearray := strings.Fields(line)
			if len(linearray) < 3 {
				ws.SendMsg("ERROR: Failed to locate GPT boot partition...")
				logger.LogToFile(
					"Unable to locate GPT boot partition..." + string(line),
				)
				return false
			}
			part := linearray[2]
			bcmd := exec.Command(
				"gpart", "bootcode", "-b", stagedir+"/boot/pmbr", "-p",
				stagedir+"/boot/gptzfsboot", "-i", part, disk,
			)
			berr := bcmd.Run()
			if berr != nil {
				ws.SendMsg(
					"Failed gpart bootcode -b " + stagedir + "/boot/pmbr -p " +
						stagedir + "/boot/gptzfsboot -i " + part + " " + disk,
				)
				logger.LogToFile(
					"Failed gpart bootcode -b " + stagedir + "/boot/pmbr -p " +
						stagedir + "/boot/gptzfsboot -i " + part + " " + disk,
				)
				return false
			}
			return true
		}
	}
	ws.SendMsg(
		"Unable to locate freebsd-boot partition on: " + string(disk),
	)
	logger.LogToFile(
		"Unable to locate freebsd-boot partition on: " + string(disk),
	)
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
		logger.LogToFile("ERROR: Failed getting kern.disks")
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
		logger.LogToFile("Updating bootloader on disk: " + kerndisks[i])
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
