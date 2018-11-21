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

// JSON structure we return to WS listeners
type UpdateInfo struct {
	New []NewPkg `json:"new"`
	Up []UpPkg `json:"update"`
	Del []DelPkg `json:"delete"`
	KernelUp bool `json:"kernelup"`
	KernelPkg string `json:"kernelpkg"`
}

// Local configuration file
type ConfigFile struct {
	Bootstrap bool `json:"trybootstrap"`
	BootstrapFatal bool `json:"bootstrapfatal"`
	OfflineUpdateKey string `json:"offlineupdatekey"`
	TrainsURL string `json:"trainsurl"`
}

// Train Def
type TrainDef struct {
	Description string `json:"description"`
	Deprecated bool `json:"deprecated"`
	Name string `json:"name"`
	NewTrain string `json:"newtrain"`
	PkgURL bool `json:"pkgurl"`
	PkgKey []string `json:"pkgkey"`
	Tags string `json:"tags"`
	Version int `json:"version"`
}

// Trains Top Level
type TrainsDef struct {
	Train []TrainDef `json:"Train"`
}
//----------------------------------------------------
