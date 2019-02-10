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

	// Set some defaults for values that may not be in the config file
	s := ConfigFile{
		Appliance: false,
		Bootstrap: false,
		BootstrapFatal: false,
		TrainsPubKey: "",
	}
	if err := json.Unmarshal(dat, &s); err != nil {
		log.Fatal(err)
	}

	// Set our gloabls now
	appliancemode = s.Appliance
	bootstrap = s.Bootstrap
	bootstrapfatal = s.BootstrapFatal
	trainsurl = s.TrainsURL

	// If we have a trains pubkey file specified for verification
	if ( s.TrainsPubKey != "" ) {
		trainpubkey = s.TrainsPubKey
	}

	// Don't update these if already set on the CLI
	if ( updatekeyflag != "" ) {
		updatekeyflag = s.OfflineUpdateKey
	}
	if ( cachedirflag != "" ) {
		cachedirflag= s.Cachedir
	}

	return true
}
