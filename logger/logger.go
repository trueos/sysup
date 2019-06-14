package logger

import (
	"github.com/trueos/sysup/defines"
	"log"
	"os"
	"os/exec"
	"strconv"
)

func RotateLog() {
	nums := []int{9, 8, 7, 6, 5, 4, 3, 2, 1}
	for _, num := range nums {
		if _, err := os.Stat(defines.LogFile + "." + strconv.Itoa(num)); os.IsNotExist(err) {
			continue
		}

		cmd := exec.Command("mv", defines.LogFile+"."+strconv.Itoa(num), defines.LogFile+"."+strconv.Itoa(num+1))
		cmd.Run()
	}

	if _, err := os.Stat(defines.LogFile); os.IsNotExist(err) {
		return
	}

	cmd := exec.Command("mv", defines.LogFile, defines.LogFile+".1")
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
