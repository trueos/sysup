package main

import (
	"log"
	"io/ioutil"
	"os"
)

func loadconfig() bool {
	// Try to load the default config file
	if _, err := os.Stat(configjson) ; os.IsNotExist(err) {
		return false
	}

	// Load the file into memory
	_, err := ioutil.ReadFile(configjson)
	if ( err != nil ) {
		log.Fatal("Failed reading configuration file: " + configjson )
	}
	return true
}
