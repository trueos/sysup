# sysup
System Update utility written in GO for TrueOS, FreeNAS, TrueView and related projects that are updated using FreeBSD's pkg base.

# Usage/Examples
- General Usage:
`sysup [-check | -update]`
- Offline Usage:
`sysup [-check | -update] -updatefile IMG_FILE [-updatekey KEY_FILE]`
- Managing Trains:
`sysup [-list-trains | -change-train TRAIN]`

## Primary Arguments (Required)
Only **one** of these arguments may be used at a time.
- **-check**
   - Check for updates
- **-update**
   - Start performing updates
- **-list-trains**
   - List all the pkg "trains" that the system is aware of (defined in "/usr/local/etc/sysup.json")
- **-change-train TRAIN_NAME**
   - Reconfigure the package repository files to point to the designated TRAIN_NAME.
   - ***WARNING*** This will remove *all* package repository configuration files on the system and create a single "/etc/pkg/Train.conf" file containing the configuration for the desired package train.

## Daemonizing the updater
- **-websocket**
   - Startup a websocket service for direct API access and events
   - This is a primary argument that should not be combined with any other flags except possibly `-addr`
- **-addr ADDRESS**
   - Websocket service address (IP:portnumber). This is a general option for all primary arguments to allow it to talk to a currently-running websocket service
   - Default value: "127.0.0.1:8134"
   
## Secondary/Optional Arguments
There are a number of secondary/optional flags that can be used for additional functionality:

### Offline Updates
sysup allows for the possibility of offline updates via an image file containing the packages from a repository. This will pull all the required packages for the update from the image file instead of trying to download packages from an online repository.
- **-updatefile IMG_FILE**
   - Run in offline mode with the designated "IMG_FILE" used as the package repository.
   - This can be used with both "-check" and "-update" primary arguments.
   - **NOTE** This ignores any trains or package repositories that are configured on the system.
- **-updatekey KEY_FILE**
   - When doing offline updates, use the "KEY_FILE" as the public SSL key to verify of the integrity of the IMG and packages.
   - Default value: none. Will not verify SSL signature of the packages.
   
### Additional Update Options
These arguments are add-ons for the "-update" argument and are typically not needed for standard use
- **-disablebootstrap**
   - Skip the update of SysUp port. This is used for running locally built SysUp and testing.
- **-bename NAME**
   - Use "NAME" for the new boot environment that will be created.
   - A boot environment with "NAME" must *not* already exist, otherwise sysup will return an error.
   - Default Value: sysup with automatically generate a unique boot environment name using a date/time stamp.
      - Example of auto-generated BE name: "2018-11-27-14-34-26"
- **-fullupdate**
   - Force a "full" update of all packages (including kernel/world).
   - Default Value: This is automatically determined based on whether the base packages (kernel/world) are tagged as newer on the package repository.
- **-stage2**
   - Start stage2 of an update (installing non-kernel package updates)
   - **WARNING** This is a debugging option that is only used internally. This should *not* be run manually by the user.
   
# TRAINS
sysup adds the ability to define package "trains". These are basically parallel package repos that might be running at different update intervals or different package configurations (as determined by the package repo maintainer(s)). Trains are considered an optional feature and are not required for single-repository update functionality.

## Local Train Configuration
- Sysup Config File: "/usr/local/etc/sysup.json"
 
This config file needs to be installed on every local system and provides the information necessary to retrieve information about available update trains.
 
### Example Config File:
```
{
  "bootstrap" : true,
  "bootstrapfatal" : false,
  "offlineupdatekey" : "/usr/share/keys/sysup-trains.pub",
  "trainsurl" : "https://my.pkg-repo.com/trains-manifest.json"
}
```

### Config File Details
- "bootstrap" (boolian) : (NOT USED YET) sysup should automatically update itself before doing any other updates
- "bootstrapfatal" (boolian) : (NOT USED YET) If the bootstrap fails, should this fail the entire update.
- "offlineupdatekey" (string) : Path to a public key file to use for offline updates. Alternative to using the "-updatekey" CLI option.
- "trainsurl" (string) : URL for where to fetch the latest manifest of available update trains.

## ONLINE TRAIN MANIFEST
This is the file publicly provided by some package repository manager or distribution, and lists all the known package repositories for their product/distribution. This manifest must be signed to ensure the integrity of the contents between the online publisher and the client system(s) which will be using it. The signature file for the trains manifest needs to be in the same directory and with the same name as the manifest but with ".sha1" on the end of the filename (example.json, example.json.sha1).

**WARNING** : The manifest contains information on both available *and* obsolete trains. If a package repo is removed, then the train should be marked as depricated *but left in the manifest*. Removing a train from the manifest may result in unexpected behavior on client systems that are set to follow that train.

### Example Manifest File
```
{
  "trains" : [
    {
      "name" : "TRAIN_NAME",
      "description" : "TRAIN_DESCRIPTION",
      "deprecated" : false,
      "newtrain" : "MOVE_TO_TRAIN_NAME",
      "pkgurl" : "http://my.pkg-repo.com/pkg/${ABI}/latest",
      "pkgkey" : [
        "-----BEGIN PUBLIC KEY-----",
        "adisugfOIYgfiutyfvuyiut6fiOIUGUHVOUVYib",
        "dgkhiou234qiyubjhvu6787tiouKJhviou875ib",
        "-----END PUBLIC KEY-----"
      ],
      "tags" : [
        "search_tag_1",
        "search_tag_2"
      ],
      "version" : 130200
    }
  ]
}
```

### Train Object Details
- "name" (string) : Name of the update train
- "description" (string) : Description of what the train provides for the end-user.
- "deprecated" (boolian) : Mark whether a repository is active or deactivated.
- "newtrain" (string) : If depricated, automatically migrate the client to this train instead.
- "pkgurl" (string) : URL for where to find the package repository
- "pkgkey" (array of strings) : Contents of the public key file used to verify packages from this repository (one line per element in the array).
- "tags" (array of strings) : List of search tags which may be used to help the user pick a train.
- "version" (number) : Artificial version number for the train. This is used to prevent people from moving from a higher-versioned train to a lower-versioned train. If there are no upgrade/downgrade limitations between trains, just set all the trains to the same version number.
