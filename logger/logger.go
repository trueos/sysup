package logger

import (
	"github.com/trueos/sysup/defines"
	"log"
	"os"
	"os/exec"
)

func RotateLog() {
	if _, err := os.Stat(defines.LogFile); os.IsNotExist(err) {
		return
	}
	cmd := exec.Command("mv", defines.LogFile, defines.LogFile+".previous")
	cmd.Run()
}

func LogToFile(info string) {
	f, err := os.OpenFile(
		defines.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644,
	)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := f.Write([]byte(info + "\n")); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}
