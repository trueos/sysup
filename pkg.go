package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/trueos/sysup/defines"
)

func getremoteosver() (string, error) {

	cmd := exec.Command(
		defines.PKGBIN, "-C", defines.PkgConf, "rquery", "-U", "%At=%Av",
		"ports-mgmt/pkg",
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
	var allText []string
	for buff.Scan() {
		allText = append(allText, buff.Text()+"\n")
	}
	//fmt.Println(allText)
	if err := cmd.Wait(); err != nil {
		exitcleanup(
			err,
			"Failed getting remote version of ports-mgmt/pkg: "+strings.Join(
				allText, "\n",
			),
		)
	}

	scanner := bufio.NewScanner(
		strings.NewReader(strings.Join(allText, "\n")),
	)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// Skip empty lines
		if len(line) == 0 {
			continue
		}
		if strings.Contains(line, "FreeBSD_version=") {
			strarray := strings.Split(line, "=")
			return string(strarray[1]), nil
		}
	}
	return "", fmt.Errorf("Failed to get FreeBSD_version", allText)
}

func mountofflineupdate() {

	// If offline update is already mounted, return
	if defines.MdDev != "" {
		logtofile("Using already mounted: " + defines.UpdateFileFlag)
		return
	}

	if _, err := os.Stat(defines.UpdateFileFlag); os.IsNotExist(err) {
		sendfatalmsg(
			"ERROR: Offline update file " + defines.UpdateFileFlag +
				" does not exist!",
		)
		closews()
		os.Exit(1)
	}

	logtofile("Mounting offline update: " + defines.UpdateFileFlag)

	output, cmderr := exec.Command(
		"mdconfig", "-a", "-t", "vnode", "-f", defines.UpdateFileFlag,
	).Output()
	if cmderr != nil {
		exitcleanup(
			cmderr, "Failed mdconfig of offline update file: "+
				defines.UpdateFileFlag,
		)
	}

	// Set the mddevice we have mounted
	defines.MdDev = strings.TrimSpace(string(output))
	//log.Println("Local MD device: " + defines.MdDev)

	derr := os.MkdirAll(defines.ImgMnt, 0755)
	if derr != nil {
		log.Fatal(derr)
	}

	cmd := exec.Command("umount", "-f", defines.ImgMnt)
	cmd.Run()

	// Mount the image RO
	cmd = exec.Command(
		"mount", "-o", "ro", "/dev/"+defines.MdDev, defines.ImgMnt,
	)
	err := cmd.Run()
	if err != nil {
		// We failed to mount, cleanup the memory device
		cmd := exec.Command("mdconfig", "-d", "-u", defines.MdDev)
		cmd.Run()
		sendfatalmsg(
			"ERROR: Offline update file " + defines.UpdateFileFlag +
				" cannot be mounted",
		)
		defines.MdDev = ""
		closews()
		os.Exit(1)
	}
}

func destroymddev() {
	if defines.UpdateFileFlag == "" {
		return
	}
	cmd := exec.Command("umount", "-f", defines.ImgMnt)
	cmd.Run()
	cmd = exec.Command("mdconfig", "-d", "-u", defines.MdDev)
	cmd.Run()
	defines.MdDev = ""
}

func mkreposfile(prefix string, pkgdb string) string {
	reposdir := "REPOS_DIR: [ \"" + pkgdb + "/repos\", ]"
	rerr := os.MkdirAll(prefix+pkgdb+"/repos", 0755)
	if rerr != nil {
		log.Fatal(rerr)
	}
	// Ugly I know, can probably be re-factored later
	pkgdata := `Update: {
url: file:///` + defines.ImgMnt
	if defines.UpdateKeyFlag != "" {
		pkgdata += `
  signature_type: "pubkey"
  pubkey: "` + defines.UpdateKeyFlag + `
`
	} else {
		pkgdata += `
  signature_type: "none"
`
	}
	pkgdata += `
  enabled: yes
}`
	ioutil.WriteFile(prefix+pkgdb+"/repos/repo.conf", []byte(pkgdata), 0644)
	return reposdir
}

func preparepkgconfig(altabi string) {
	derr := os.MkdirAll(defines.PkgDb, 0755)
	if derr != nil {
		exitcleanup(derr, "Failed making directory: "+defines.PkgDb)
	}
	cerr := os.MkdirAll(defines.CacheDir, 0755)
	if cerr != nil {
		exitcleanup(cerr, "Failed making directory: "+defines.CacheDir)
	}

	// If we have an offline file update, lets set that up now
	var reposdir string
	if defines.UpdateFileFlag != "" {
		mountofflineupdate()
		reposdir = mkreposfile("", defines.PkgDb)
	}

	// Check if we have an alternative ABI to specify
	if altabi != "" {
		defines.AbiOverride = "ABI: " + altabi
	}

	// Copy over the existing local database
	srcFolder := "/var/db/pkg/local.sqlite"
	destFolder := defines.PkgDb + "/local.sqlite"
	cpCmd := exec.Command("cp", "-f", srcFolder, destFolder)
	err := cpCmd.Run()
	if err != nil {
		exitcleanup(err, "Failed copy of /var/db/pkg/local.sqlite")
	}

	// Create the config file
	fdata := `PKG_CACHEDIR: ` + defines.CacheDir + `
PKG_DBDIR: ` + defines.PkgDb + `
IGNORE_OSVERSION: YES
` + reposdir + `
` + defines.AbiOverride
	ioutil.WriteFile(defines.PkgConf, []byte(fdata), 0644)
}

func updatepkgdb(newabi string) {
	cmd := exec.Command(defines.PKGBIN, "-C", defines.PkgConf, "update", "-f")
	if newabi == "" {
		sendinfomsg("Updating package remote database")
	} else {
		sendinfomsg("Updating package remote database with new ABI: " + newabi)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		exitcleanup(err, "Failed updating package remote DB")
	}
	if err := cmd.Start(); err != nil {
		exitcleanup(err, "Failed starting update of package remote DB")
	}
	buff := bufio.NewScanner(stderr)
	// Iterate over buff and append content to the slice
	var allText []string
	abierr := false
	for buff.Scan() {
		line := buff.Text()
		if strings.Contains(line, "wrong ABI:") && newabi == "" {
			words := strings.Split(string(line), " ")
			if len(words) < 8 {
				logtofile("Unable to determine new ABI")
				log.Fatal("Unable to determine new ABI")
			}
			abierr = true
			//fmt.Println("New ABI: " + words[8])
			// Try updating with the new ABI now
			preparepkgconfig(words[8])
			updatepkgdb(words[8])
		}
		allText = append(allText, line+"\n")
	}
	if err := cmd.Wait(); err != nil {
		if !abierr {
			fmt.Println(allText)
			logtofile(
				"Failed running pkg update: " + strings.Join(allText, "\n"),
			)
			exitcleanup(
				err, "Failed running pkg update:"+strings.Join(allText, "\n"),
			)
		}
	}
}

func exitcleanup(err error, text string) {
	// If we are using standalone update, cleanup
	if defines.UpdateFileFlag != "" && defines.MdDev != "" {
		destroymddev()
	}
	log.Println("ERROR: " + text)
	log.Fatal(err)
}
