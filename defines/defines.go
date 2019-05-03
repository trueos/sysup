package defines

import (
	"flag"
	"github.com/gorilla/websocket"
)

var Updater = websocket.Upgrader{} // use default options
// Start our client connection to the WS server
var WSServer *websocket.Conn
var WSClient *websocket.Conn

var pkgflags string

// What is this tool called?
var toolname = "sysup"

// Where to log by default
var LogFile = "/var/log/" + toolname + ".log"

// Configuration file location
var ConfigJson = "/usr/local/etc/" + toolname + ".json"

// Global trains URL
var TrainsUrl string

// Set our default bootstrap options
var Bootstrap = false
var BootstrapFatal = false

// Default pubkey used for trains
var TrainPubKey = "/usr/local/share/" + toolname + "/trains.pub"

// Package defaults
//----------------------------------------------------
var PKGBIN = "pkg-static"

var SysUpDb = "/var/db/" + toolname
var PkgDb = SysUpDb + "/pkgdb"
var ImgMnt = SysUpDb + "/mnt"
var PkgConf = SysUpDb + "/pkg.conf"
var CacheDir = SysUpDb + "/cache"
var MdDev = ""
var AbiOverride = ""

//----------------------------------------------------

// Boot-Environment defaults
//----------------------------------------------------
var BEBIN = "beadm"
var BESTAGE = "updatego-stage"
var STAGEDIR = "/.updatestage"

//----------------------------------------------------

// Setup our CLI Flags
//----------------------------------------------------
var BeNameFlag string
var BootloaderFlag bool
var ChangeTrainFlag string
var CheckFlag bool
var DisableBsFlag bool
var FullUpdateFlag bool
var ListTrainFlag bool
var UpdateFlag bool
var UpdateFileFlag string
var UpdateKeyFlag string
var CacheDirFlag string
var WebsocketFlag bool
var WebsocketAddr string

func init() {
	flag.BoolVar(
		&CheckFlag,
		"check",
		false,
		"Check system status",
	)
	flag.BoolVar(
		&DisableBsFlag,
		"disablebootstrap",
		false,
		"Disable bootstrap of sysup package on upgrade",
	)
	flag.BoolVar(
		&UpdateFlag,
		"update",
		false,
		"Update to latest packages",
	)

	flag.BoolVar(
		&ListTrainFlag,
		"list-trains",
		false,
		"List available trains (if configured)",
	)
	flag.StringVar(
		&ChangeTrainFlag,
		"change-train",
		"",
		"Change to the specifed new train",
	)
	flag.BoolVar(
		&FullUpdateFlag,
		"fullupdate",
		false,
		"Force a full update",
	)
	flag.BoolVar(
		&BootloaderFlag,
		"updatebootloader",
		false,
		"Perform one-time update of bootloader",
	)
	flag.StringVar(
		&UpdateFileFlag,
		"updatefile",
		"",
		"Use the specified update image instead of fetching from remote",
	)
	flag.StringVar(
		&UpdateKeyFlag,
		"updatekey",
		"",
		"Use the specified update pubkey for offline updates"+
			" (Defaults to none)",
	)
	flag.StringVar(
		&BeNameFlag,
		"bename",
		"",
		"Set the name of the new boot-environment for updating."+
			" Must not exist yet.",
	)
	flag.StringVar(
		&CacheDirFlag,
		"cachedir",
		"",
		"Set the temp data location where we download files / cache data",
	)
	flag.BoolVar(
		&WebsocketFlag,
		"websocket",
		false,
		"Start websocket server for direct API access and events",
	)
	flag.StringVar(
		&WebsocketAddr,
		"websocket-addr",
		"127.0.0.1:8134",
		"Address to have the websocket server listen on",
	)
	flag.Parse()
}

func SetLocs() {
	// Check if the user provided their own location to store temp data
	if CacheDirFlag == "" {
		return
	}

	SysUpDb = CacheDirFlag
	PkgDb = SysUpDb + "/pkgdb"
	ImgMnt = SysUpDb + "/mnt"
	PkgConf = SysUpDb + "/pkg.conf"
	CacheDir = SysUpDb + "/cache"

}

// Define all our JSON structures
//----------------------------------------------------
type NewPkg struct {
	Name    string `json:"name"`
	Version string `json:"Version"`
}

type UpPkg struct {
	Name       string `json:"name"`
	OldVersion string `json:"OldVersion"`
	NewVersion string `json:"NewVersion"`
}

type RiPkg struct {
	Name   string `json:"name"`
	Reason string `json:"Reason"`
}

type DelPkg struct {
	Name    string `json:"name"`
	Version string `json:"Version"`
}

// Local configuration file
type ConfigFile struct {
	Bootstrap        bool   `json:"bootstrap"`
	BootstrapFatal   bool   `json:"bootstrapfatal"`
	CacheDir         string `json:"cachedir"`
	OfflineUpdateKey string `json:"offlineupdatekey"`
	TrainsURL        string `json:"trainsurl"`
	TrainsPubKey     string `json:"trainspubkey"`
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
	Description string   `json:"description"`
	Deprecated  bool     `json:"deprecated"`
	Name        string   `json:"name"`
	NewTrain    string   `json:"newtrain"`
	PkgURL      string   `json:"pkgurl"`
	PkgKey      []string `json:"pkgkey"`
	Tags        []string `json:"tags"`
	Version     int      `json:"version"`
	Current     bool     `json:"current"`
}

// Trains Top Level
type TrainsDef struct {
	Trains  []TrainDef `json:"trains"`
	Default string     `json:"default"`
}

// Update information we return to API requests
type UpdateInfo struct {
	New       []NewPkg `json:"new"`
	Up        []UpPkg  `json:"update"`
	Ri        []RiPkg  `json:"reinstall"`
	Del       []DelPkg `json:"delete"`
	KernelUp  bool     `json:"kernelup"`
	KernelPkg string   `json:"kernelpkg"`
	SysUp     bool     `json:"sysup"`
	SysUpPkg  string   `json:"sysuppkg"`
}

// Incoming JSON API Requests
//----------------------------------------------------

// Generic API request to handle check/update/list-trains/set-train via the
// Method property
type SendReq struct {
	Method     string `json:"method"`
	Bename     string `json:"bename"`
	Disablebs  bool   `json:"disablebs"`
	Fullupdate bool   `json:"fullupdate"`
	Cachedir   string `json:"cachedir"`
	Train      string `json:"train"`
	Updatefile string `json:"updatefile"`
	Updatekey  string `json:"updatekey"`
}

//----------------------------------------------------
