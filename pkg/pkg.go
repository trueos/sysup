package pkg

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/trueos/sysup/defines"
	"github.com/trueos/sysup/logger"
	"github.com/trueos/sysup/ws"
)

var KernelPkg string = ""

func GetRemoteOsVer() (string, error) {

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
	//log.Println(allText)
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
		logger.LogToFile("Using already mounted: " + defines.UpdateFileFlag)
		return
	}

	if _, err := os.Stat(defines.UpdateFileFlag); os.IsNotExist(err) {
		ws.SendMsg(
			"Offline update file "+defines.UpdateFileFlag+
				" does not exist!",
			"fatal",
		)
		ws.CloseWs()
		// TODO: Do we really want to kill the server?
		os.Exit(1)
	}

	logger.LogToFile("Mounting offline update: " + defines.UpdateFileFlag)

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
		ws.SendMsg(
			"Offline update file "+defines.UpdateFileFlag+
				" cannot be mounted",
			"fatal",
		)
		defines.MdDev = ""
		ws.CloseWs()
		// TODO: Do we really want to kill the server?
		os.Exit(1)
	}
}

func DestroyMdDev() {
	if defines.UpdateFileFlag == "" {
		return
	}
	cmd := exec.Command("umount", "-f", defines.ImgMnt)
	cmd.Run()
	cmd = exec.Command("mdconfig", "-d", "-u", defines.MdDev)
	cmd.Run()
	defines.MdDev = ""
}

func MkReposFile(prefix string, pkgdb string) string {
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

func PreparePkgConfig(altabi string) {
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
		reposdir = MkReposFile("", defines.PkgDb)
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

func UpdatePkgDb(newabi string) {
	cmd := exec.Command(defines.PKGBIN, "-C", defines.PkgConf, "update", "-f")
	if newabi == "" {
		ws.SendMsg("Updating package remote database")
	} else {
		ws.SendMsg("Updating package remote database with new ABI: " + newabi)
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
				logger.LogToFile("Unable to determine new ABI")
				log.Fatal("Unable to determine new ABI")
			}
			abierr = true
			//log.Println("New ABI: " + words[8])
			// Try updating with the new ABI now
			PreparePkgConfig(words[8])
			UpdatePkgDb(words[8])
		}
		allText = append(allText, line+"\n")
	}
	if err := cmd.Wait(); err != nil {
		if !abierr {
			log.Println(allText)
			logger.LogToFile(
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
		DestroyMdDev()
	}
	log.Println("ERROR: " + text)
	log.Fatal(err)
}

func UpdateDryRun(sendupdate bool) (*defines.UpdateInfo, bool, error) {
	details := defines.UpdateInfo{}
	updetails := &details

	cmd := exec.Command(defines.PKGBIN, "-C", defines.PkgConf, "upgrade", "-n")
	ws.SendMsg("Checking system for updates")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		ws.SendMsg("Failed dry run of pkg upgrade", "fatal")
		return updetails, false, errors.New("ERROR")
	}
	if err := cmd.Start(); err != nil {
		ws.SendMsg("Failed dry run of pkg upgrade", "fatal")
		return updetails, false, errors.New("ERROR")
	}
	buff := bufio.NewScanner(stdout)

	// Iterate over buff and append content to the slice
	var allText []string
	for buff.Scan() {
		allText = append(allText, buff.Text()+"\n")
	}
	//log.Println(allText)
	// Pkg returns 0 on sucess and 1 on updates needed
	//if err := cmd.Wait(); err != nil {
	//	log.Fatal(err)
	//}

	haveupdates := !strings.Contains(strings.Join((allText), "\n"), "Your packages are up to date")
	if haveupdates {
		updetails = ParseUpdateData(allText)
	}

	return updetails, haveupdates, nil
}

func ParseUpdateData(uptext []string) *defines.UpdateInfo {
	var stage string
	var line string

	// Init the structure
	details := defines.UpdateInfo{}
	detailsNew := defines.NewPkg{}
	detailsUp := defines.UpPkg{}
	detailsRi := defines.RiPkg{}
	detailsDel := defines.DelPkg{}

	scanner := bufio.NewScanner(strings.NewReader(strings.Join(uptext, "\n")))
	for scanner.Scan() {
		line = scanner.Text()
		line = strings.TrimSpace(line)

		// Skip empty lines
		if len(line) == 0 {
			continue
		}
		if strings.Contains(line, "INSTALLED:") {
			stage = "NEW"
			continue
		}
		if strings.Contains(line, "UPGRADED:") {
			stage = "UPGRADE"
			continue
		}
		if strings.Contains(line, "REMOVED:") {
			stage = "REMOVE"
			continue
		}
		if strings.Contains(line, "REINSTALLED:") {
			stage = "REINSTALLED"
			continue
		}
		if strings.Contains(line, " to be installed:") {
			stage = ""
			continue
		}
		if strings.Contains(line, " to be upgraded:") {
			stage = ""
			continue
		}
		if strings.Contains(line, " to be REINSTALLED:") {
			stage = ""
			continue
		}
		//		log.Printf(line + "\n")
		//		log.Printf("Fields are: %q\n", strings.Fields(line))
		switch stage {
		case "NEW":
			if strings.Contains(line, ": ") {
				linearray := strings.Split(line, " ")
				if len(linearray) < 2 {
					continue
				}
				detailsNew.Name = linearray[0]
				detailsNew.Version = linearray[1]
				details.New = append(details.New, detailsNew)
				continue
			}
		case "UPGRADE":
			if strings.Contains(line, " -> ") {
				linearray := strings.Split(line, " ")
				if len(linearray) < 4 {
					continue
				}
				detailsUp.Name = strings.Replace(linearray[0], ":", "", -1)
				detailsUp.OldVersion = linearray[1]
				detailsUp.NewVersion = linearray[3]
				details.Up = append(details.Up, detailsUp)
				continue
			}
		case "REINSTALLED":
			if strings.Contains(line, " (") {
				linearray := strings.Split(line, " (")
				if len(linearray) < 2 {
					continue
				}
				detailsRi.Name = linearray[0]
				detailsRi.Reason = strings.Replace(linearray[1], ")", "", -1)
				details.Ri = append(details.Ri, detailsRi)
				continue
			}
		case "REMOVE":
			if strings.Contains(line, ": ") {
				linearray := strings.Split(line, " ")
				if len(linearray) < 2 {
					continue
				}
				detailsDel.Name = linearray[0]
				detailsDel.Version = linearray[1]
				details.Del = append(details.Del, detailsDel)
				continue
			}
		default:
		}
	}

	// Search if a kernel is apart of this update
	kernel := GetKernelPkgName()
	details.KernelPkg = kernel
	details.KernelUp = false
	log.Println("Kernel: " + kernel)
	for i, _ := range details.Up {
		if details.Up[i].Name == kernel {
			// Set JSON details on the kernel update
			details.KernelUp = true
			break
		}
	}

	// Check if we have a SysUp package to update
	details.SysUpPkg = ""
	details.SysUp = false
	for i, _ := range details.Up {
		if details.Up[i].Name == "sysup" {
			// Set JSON details on the sysup package
			details.SysUp = true
			break
		}
	}

	// If we have a remote ABI change we count that as a new kernel change also
	if HaveOsVerChange() {
		details.KernelUp = true
	}

	//	log.Print("UpdateInfo", details)
	return &details
}

func GetKernelPkgName() string {
	logger.LogToFile("Checking kernel package name")
	kernfile, kerr := syscall.Sysctl("kern.bootfile")
	if kerr != nil {
		logger.LogToFile("Failed getting kern.bootfile")
		log.Fatal(kerr)
	}
	kernpkgout, perr := exec.Command(
		defines.PKGBIN, "which", kernfile,
	).Output()
	if perr != nil {
		logger.LogToFile("Failed which " + kernfile)
		log.Fatal(perr)
	}
	kernarray := strings.Split(string(kernpkgout), " ")
	if len(kernarray) < 6 {
		logger.LogToFile("Unable to determine kernel package name")
		log.Fatal("Unable to determine kernel package name")
	}
	kernpkg := kernarray[5]
	kernpkg = strings.TrimSpace(kernpkg)
	logger.LogToFile("Local Kernel package: " + kernpkg)
	shellcmd := defines.PKGBIN + " info " + kernpkg +
		" | grep '^Name' | awk '{print $3}'"
	cmd := exec.Command("/bin/sh", "-c", shellcmd)
	kernpkgname, err := cmd.Output()
	if err != nil {
		log.Println(fmt.Sprint(err) + ": " + string(kernpkgname))
		log.Fatal("ERROR query of kernel package name")
	}
	kernel := strings.TrimSpace(string(kernpkgname))

	logger.LogToFile("Kernel package: " + kernel)
	return kernel
}
