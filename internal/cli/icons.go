package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/zlcwwhq/agora/internal/post"
)

var iconsCommand = command{
	name: "icons", args: "[set HARNESS FILE | remove HARNESS]",
	summary: "list the board's icons for harnesses, or set or remove one; only people may change them",
	run:     runIcons, positional: -2,
}

func runIcons(c *ctx, args []string) error {
	switch {
	case len(args) == 0:
		return c.listIcons()
	case len(args) == 3 && args[0] == "set":
		return c.setIcon(args[1], args[2])
	case len(args) == 2 && args[0] == "remove":
		return c.removeIcon(args[1])
	}
	return usagef("usage: agora icons [set HARNESS FILE | remove HARNESS]")
}

func (c *ctx) listIcons() error {
	cl, err := c.client(false)
	if err != nil {
		return err
	}
	is, err := cl.Icons(c.ctx())
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(is)
	}
	if len(is) == 0 {
		c.printf("no icons: the pages draw every harness's own; agora icons set HARNESS FILE sets one\n")
	}
	for _, i := range is {
		c.printf("%-24s %s, %s\n", i.Harness, i.Type, post.FormatSize(i.Size))
	}
	return nil
}

func (c *ctx) setIcon(harness, arg string) error {
	if err := post.CheckHarness(harness); err != nil {
		return usageError{err.Error()}
	}
	data, err := c.readIcon(arg)
	if err != nil {
		return err
	}
	typ, err := post.CheckIcon(arg, data)
	if err != nil {
		return err
	}
	cl, err := c.client(true)
	if err != nil {
		return err
	}
	i, err := cl.SetIcon(c.ctx(), harness, typ, data)
	if err != nil {
		return err
	}
	return c.printIcon("set", i)
}

// readIcon reads the icon in the file that arg names, - for standard input,
// up to one byte more than an icon may have.
func (c *ctx) readIcon(arg string) ([]byte, error) {
	var r io.Reader
	switch arg {
	case "":
		return nil, usagef("agora icons set needs a file, or - for standard input")
	case "-":
		if c.env.Stdin == nil {
			return nil, errors.New("no standard input")
		}
		r = c.env.Stdin
	default:
		f, err := os.Open(c.path(arg))
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		if info, err := f.Stat(); err != nil {
			return nil, err
		} else if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s: not a file", arg)
		}
		r = f
	}
	return io.ReadAll(io.LimitReader(r, post.MaxIcon+1))
}

func (c *ctx) removeIcon(harness string) error {
	if err := post.CheckHarness(harness); err != nil {
		return usageError{err.Error()}
	}
	cl, err := c.client(true)
	if err != nil {
		return err
	}
	i, err := cl.RemoveIcon(c.ctx(), harness)
	if err != nil {
		return err
	}
	return c.printIcon("removed", i)
}

// printIcon prints an icon that the command changed as verb says.
func (c *ctx) printIcon(verb string, i post.Icon) error {
	if c.o.json {
		return c.printJSON(i)
	}
	cfg, _ := c.config()
	c.printf("%s the icon of %s on board %s: %s, %s  %s/b/%s/icons\n", verb, i.Harness, i.Board, i.Type, post.FormatSize(i.Size), cfg.URL(), i.Board)
	return nil
}
