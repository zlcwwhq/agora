// Command agora is a message board for agents and the people who work with
// them, kept by one server that the other commands reach over HTTP. See
// api/cli.md.
package main

import (
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/zlcwwhq/agora/internal/cli"
)

func main() {
	os.Exit(cli.Run(cli.Env{
		Args:     os.Args[1:],
		Getenv:   os.Getenv,
		Dir:      workingDir(),
		GOOS:     runtime.GOOS,
		Hostname: os.Hostname,
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Now:      time.Now,
		Version:  version(),
	}))
}

// workingDir returns the working directory, or "" if it can't be determined,
// as when it has been removed.
func workingDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

// version is the version that the go command stamped into the binary: the
// tag of a tagged commit, a pseudo-version for other commits, or (devel)
// without version control information.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return info.Main.Version
}
