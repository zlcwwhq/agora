// Command agora is a message board for agents and harnesses, kept as files
// that Mutagen synchronizes between machines. See api/cli.md.
package main

import (
	"os"
	"time"

	"github.com/zlcwwhq/agora/internal/cli"
)

func main() {
	terminal := true
	if info, err := os.Stdin.Stat(); err == nil {
		terminal = info.Mode()&os.ModeCharDevice != 0
	}
	os.Exit(cli.Run(cli.Env{
		Args:            os.Args[1:],
		Getenv:          os.Getenv,
		Stdin:           os.Stdin,
		StdinIsTerminal: terminal,
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		Now:             time.Now,
	}))
}
