package utils

import (
	"fmt"
	"io"
	"net"
	"os"
)

func Copyfile(src, dst string) (int64, error) {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return 0, err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer destination.Close()
	nBytes, err := io.Copy(destination, source)
	return nBytes, err
}

func GetFreePort() (int, error) {
	ln, err := net.Listen("tcp", ":0")
	defer ln.Close()

	if err != nil {
		return 0, err
	}

	return ln.Addr().(*net.TCPAddr).Port, nil
}
