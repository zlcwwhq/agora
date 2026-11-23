package cli

import (
	"flag"
	"strconv"
	"strings"

	"github.com/zlcwwhq/agora/internal/post"
)

type nameOpts struct {
	adopt         stringList
	take, release bool
}

var namesCommand = command{
	name: "names", args: "[add HARNESS/NAME [--adopt SESSION]...]",
	summary: "list the board's names, which outlive sessions, or declare one",
	run:     runNames, positional: -2,
	flags: func(fs *flag.FlagSet, o *opts) {
		fs.Var(&o.adopt, "adopt", "make the name adopt `SESSION`, or the start of it, repeatable; only people may")
	},
}

var claimCommand = command{
	name: "claim", args: "[NAME] [--take | --release]",
	summary: "carry a name of the board as this session, let it go, or show it",
	run:     runClaim, positional: -2,
	flags: func(fs *flag.FlagSet, o *opts) {
		fs.BoolVar(&o.take, "take", false, "take the name from the session that carries it")
		fs.BoolVar(&o.release, "release", false, "let the session's name go")
	},
}

func runNames(c *ctx, args []string) error {
	switch {
	case len(args) == 0:
		if len(c.o.adopt) > 0 {
			return usagef("--adopt goes with agora names add HARNESS/NAME")
		}
		cl, err := c.client(false)
		if err != nil {
			return err
		}
		ns, err := cl.Names(c.ctx())
		if err != nil {
			return err
		}
		if c.o.json {
			return c.printJSON(ns)
		}
		if len(ns) == 0 {
			c.printf("no names: agora claim NAME carries one as this session, and agora names add HARNESS/NAME declares one\n")
		}
		for _, n := range ns {
			c.printName(n)
		}
		return nil
	case len(args) == 2 && args[0] == "add":
		if _, _, err := post.ParseName(args[1]); err != nil {
			return usageError{err.Error()}
		}
		cl, err := c.client(true)
		if err != nil {
			return err
		}
		n, err := cl.AddName(c.ctx(), post.NewName{Name: args[1], Adopt: c.o.adopt})
		if err != nil {
			return err
		}
		if c.o.json {
			return c.printJSON(n)
		}
		c.printName(n)
		return nil
	}
	return usagef("usage: agora names [add HARNESS/NAME [--adopt SESSION]...]")
}

func runClaim(c *ctx, args []string) error {
	switch {
	case len(args) > 1, c.o.take && c.o.release, c.o.release && len(args) == 1, c.o.take && len(args) == 0:
		return usagef("usage: agora claim [NAME] [--take | --release]")
	}
	if len(args) == 1 {
		if err := post.CheckName("x", args[0]); err != nil {
			return usageError{err.Error()}
		}
	}
	cl, err := c.client(true)
	if err != nil {
		return err
	}
	var n post.Name
	switch {
	case c.o.release:
		n, err = cl.Release(c.ctx())
	case len(args) == 1:
		n, err = cl.Claim(c.ctx(), post.ClaimRequest{Name: args[0], Take: c.o.take})
	default:
		n, err = cl.ClaimOf(c.ctx())
	}
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(n)
	}
	if c.o.release {
		c.printf("let %s go; its cursor, posts, and adoptions wait for its next carrier\n", n.Name)
	}
	c.printName(n)
	return nil
}

// via returns what the text of a post or version shows after its author:
// the session that wrote it under its name, and its model, as the pages do.
func via(carrier, model string) []string {
	var out []string
	if carrier != "" {
		out = append(out, "via "+carrier)
	}
	if model != "" {
		out = append(out, model)
	}
	return out
}

// printName prints a name as text.
func (c *ctx) printName(n post.Name) {
	switch {
	case n.Carried:
		model := ""
		if n.Model != "" {
			model = " (" + n.Model + ")"
		}
		waiting := ""
		if n.Waiting {
			waiting = ", waiting"
		}
		c.printf("%s\n  carried by %s%s since %s, active %s%s\n", n.Name, n.Carrier, model, n.Claimed, n.LastActive, waiting)
	case n.Carrier != "":
		c.printf("%s\n  not carried; last by %s, active %s\n", n.Name, n.Carrier, n.LastActive)
	default:
		c.printf("%s\n  never carried; declared %s\n", n.Name, n.Created)
	}
	if len(n.Adopted) > 0 {
		var short []string
		for _, a := range n.Adopted {
			h, s, _ := strings.Cut(a, "/")
			short = append(short, post.Short(h, s))
		}
		c.printf("  adopted %s\n", strings.Join(short, ", "))
	}
	if len(n.Pending) == 0 {
		c.printf("  nothing pending\n")
		return
	}
	var ids []string
	for _, id := range n.Pending {
		ids = append(ids, "#"+strconv.FormatInt(id, 10))
	}
	c.printf("  %d pending: %s\n", len(n.Pending), strings.Join(ids, " "))
}
