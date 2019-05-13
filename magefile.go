//+build mage

package main

import (
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
	"log"
)

// TODO: Use Test() later

// Uses go fmt to format all our modules
func Format() {
	log.Println("Running format")

	sh.RunV("go", "fmt", "./...")
}

// Installs our formatting deps
func InstallDeps() {
	log.Println("Installing deps")
	sh.Run(
		"go", "get", "github.com/fzipp/gocyclo",
		"github.com/gordonklaus/ineffassign", "github.com/client9/misspell",
		"golang.org/x/lint/golint",
	)
}

// Runs the linting process
func Lint() {
	log.Println("Running lint")
	sh.RunV("go", "vet", "./...")
	sh.RunV(
		"gocyclo", "-over", "15", "pkg", "logger", "trains", "update",
		"utils", "ws", "defines", "client",
	)
	sh.RunV("golint", "./...")
	sh.RunV("ineffassign", "./")
	sh.RunV("misspell", "-w", "./")
}

// Cleans sysups binary up
func Clean() {
	log.Println("Running clean")
	sh.RunV("go", "clean")
	sh.RunV("rm", "-f", "./sysup")
}

// Builds sysup and run it from this directory
func Run() {
	log.Println("Running run")
	mg.Deps(Build)
	sh.Run("./sysup")
}

// Installs sysup to GOPATH
func Install() {
	log.Println("Running install")
	sh.RunV("go", "install")
}

// Run our tests (nonfunctional)
func Test() {
	log.Println("Running test")
	sh.RunV("go", "test", "-v", "./...")
}

// Builds sysup and install it
func All() {
	log.Println("Making all")
	mg.Deps(Build, Install)
}

// Builds sysup
func Build() {
	log.Println("Making build")
	sh.RunV("go", "build", "-o", "sysup", "-v")
}

// Standard development build process (format, install deps, lint, build)
func Dev() {
	log.Println("Making dev")
	mg.Deps(Format, InstallDeps, Lint, Build)
}
