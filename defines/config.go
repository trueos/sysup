package defines

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
)

func LoadConfig() bool {
	// Try to load the default config file
	if _, err := os.Stat(ConfigJson); os.IsNotExist(err) {
		return false
	}

	// Load the file into memory
	dat, err := ioutil.ReadFile(ConfigJson)
	if err != nil {
		log.Fatal("Failed reading configuration file: " + ConfigJson)
	}

	// Set some defaults for values that may not be in the config file
	s := ConfigFile{
		Bootstrap:      false,
		BootstrapFatal: false,
		TrainsPubKey:   "",
	}
	if err := json.Unmarshal(dat, &s); err != nil {
		log.Fatal(err)
	}

	// Set our gloabls now
	Bootstrap = s.Bootstrap
	BootstrapFatal = s.BootstrapFatal
	TrainsUrl = s.TrainsURL

	// If we have a trains pubkey file specified for verification
	if s.TrainsPubKey != "" {
		TrainPubKey = s.TrainsPubKey
	}

	// Don't update these if already set on the CLI
	if UpdateKeyFlag != "" {
		UpdateKeyFlag = s.OfflineUpdateKey
	}

	if CacheDirFlag != "" {
		s.CacheDir = CacheDirFlag
	}

	return true
}
