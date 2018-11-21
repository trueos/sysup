package main

import (
	"flag"
	"github.com/gorilla/websocket"
)

var (
	addr    = flag.String("addr", "127.0.0.1:8134", "Websocket service address")
)

var updater = websocket.Upgrader{} // use default options
// Start our client connection to the WS server
var (
        conns   *websocket.Conn
)
var pkgflags string


// What is this tool called?
var toolname = "sysup"

// Where to log by default
var logfile = "/var/log/" + toolname + ".log"

// Configuration file location
var configjson = "/usr/local/etc/" + toolname + ".json"

// Global trains URL
var trainsurl string

// Set our default bootstrap options
var bootstrap = false
var bootstrapfatal = false

// Package defaults
//----------------------------------------------------
var PKGBIN = "pkg-static"
var localpkgdb = "/var/db/" + toolname + "/pkgdb"
var localimgmnt = "/var/db/" + toolname + "/mnt"
var localpkgconf = "/var/db/" + toolname + "/pkg.conf"
var localcachedir = "/var/cache/" + toolname
var localmddev = ""
//----------------------------------------------------

// Boot-Environment defaults
//----------------------------------------------------
var BEBIN = "beadm"
var BESTAGE = "updatego-stage"
var STAGEDIR = "/.updatestage"
//----------------------------------------------------

// Setup our CLI Flags
//----------------------------------------------------
var benameflag string
var changetrainflag string
var checkflag bool
var fullupdateflag bool
var listtrainflag bool
var stage2flag bool
var updateflag bool
var updatefileflag string
var updatekeyflag string
var websocketflag bool
func init() {
	flag.BoolVar(&checkflag, "check", false, "Check system status")
	flag.BoolVar(&updateflag, "update", false, "Update to latest packages")
	flag.BoolVar(&listtrainflag, "list-trains", false, "List available trains (if configured)")
	flag.StringVar(&changetrainflag, "change-train", "", "Change to the specifed new train")
	flag.BoolVar(&fullupdateflag, "fullupdate", false, "Force a full update")
	flag.BoolVar(&stage2flag, "stage2", false, "Start stage2 of an update (Normally used internally only)")
	flag.StringVar(&updatefileflag, "updatefile", "", "Use the specified update image instead of fetching from remote")
	flag.StringVar(&updatekeyflag, "updatekey", "", "Use the specified update pubkey for offline updates (Defaults to none)")
	flag.StringVar(&benameflag, "bename", "", "Set the name of the new boot-environment for updating. Must not exist yet.")
	flag.BoolVar(&websocketflag, "websocket", false, "Start websocket server for direct API access and events")
	flag.Parse()
}

// Define all our JSON structures
//----------------------------------------------------
type NewPkg struct {
	Name string `json:"name"`
	Version string `json:"Version"`
}

type UpPkg struct {
	Name string `json:"name"`
	OldVersion string `json:"OldVersion"`
	NewVersion string `json:"NewVersion"`
}

type DelPkg struct {
	Name string `json:"name"`
	Version string `json:"Version"`
}

// Local configuration file
type ConfigFile struct {
	Bootstrap bool `json:"bootstrap"`
	BootstrapFatal bool `json:"bootstrapfatal"`
	OfflineUpdateKey string `json:"offlineupdatekey"`
	TrainsURL string `json:"trainsurl"`
}


type Envelope struct {
	Method string
}

// Outgoing JSON API Responses
//----------------------------------------------------

// Return API of check request
type Check struct {
	Updates bool
	Details UpdateInfo
}

// Return informational message
type InfoMsg struct {
	Info string
}

// Train Def
type TrainDef struct {
	Description string `json:"description"`
	Deprecated bool `json:"deprecated"`
	Name string `json:"name"`
	NewTrain string `json:"newtrain"`
	PkgURL string `json:"pkgurl"`
	PkgKey []string `json:"pkgkey"`
	Tags []string `json:"tags"`
	Version int `json:"version"`
	Current bool `json:"current"`
}

// Trains Top Level
type TrainsDef struct {
	Trains []TrainDef `json:"trains"`
}

// Update information we return to API requests
type UpdateInfo struct {
	New []NewPkg `json:"new"`
	Up []UpPkg `json:"update"`
	Del []DelPkg `json:"delete"`
	KernelUp bool `json:"kernelup"`
	KernelPkg string `json:"kernelpkg"`
}


// Incoming JSON API Requests
//----------------------------------------------------

// Generic API request to handle check/update/list-trains via the Method property
type UpdateReq struct {
	Method string `json:"method"`
	Bename string `json:"bename"`
	Fullupdate bool `json:"fullupdate"`
	Updatefile string `json:"updatefile"`
	Updatekey string `json:"updatekey"`
}

//----------------------------------------------------
