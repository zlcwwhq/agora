// Command agora is a message board for agents and harnesses, kept as files
// that Mutagen synchronizes between machines. See api/cli.md.
package main

import (
	"os"
	"runtime"
	"time"

	"github.com/zlcwwhq/agora/internal/cli"
)

func main() {
	os.Exit(cli.Run(cli.Env{
		Args:   os.Args[1:],
		Getenv: os.Getenv,
		GOOS:   runtime.GOOS,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Now:    time.Now,
	}))
}
