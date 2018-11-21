package main

import (
	"encoding/json"
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
	dat, err := ioutil.ReadFile(configjson)
	if ( err != nil ) {
		log.Fatal("Failed reading configuration file: " + configjson )
	}

	s := ConfigFile{
		Bootstrap: false,
		BootstrapFatal: false,
	}
	if err := json.Unmarshal(dat, &s); err != nil {
		log.Fatal(err)
	}
	bootstrap = s.Bootstrap
	bootstrapfatal = s.BootstrapFatal
	updatekeyflag = s.OfflineUpdateKey
	trainsurl = s.TrainsURL

	return true
}
