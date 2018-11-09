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

// Package defaults
//----------------------------------------------------
var PKGBIN = "pkg-static"
var localpkgdb = "/var/db/update-go/pkgdb"
var localpkgconf = "/var/db/update-go/pkg.conf"
var localcachedir = "/var/cache/update-go"
//----------------------------------------------------

// Boot-Environment defaults
//----------------------------------------------------
var BEBIN = "beadm"
var BESTAGE = "updatego-stage"
var STAGEDIR = "/.updatestage"
//----------------------------------------------------

// Define all our JSON structures we use to return update info
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

type UpdateInfo struct {
	New []NewPkg `json:"new"`
	Up []UpPkg `json:"update"`
	Del []DelPkg `json:"delete"`
}
//----------------------------------------------------
