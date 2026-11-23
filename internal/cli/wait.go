package cli

import (
	"flag"
	"fmt"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
)

type waitOpts struct {
	timeout string
}

var waitCommand = command{
	name: "wait", args: "[--to-me] [--thread ID] [--peek] [--timeout DURATION]",
	summary: "wait until unread would show posts, then show them as it does",
	run:     runWait,
	flags: func(fs *flag.FlagSet, o *opts) {
		fs.BoolVar(&o.toMe, "to-me", false, "only the posts of threads where this reader posted, and those that mention it")
		fs.StringVar(&o.thread, "thread", "", "only the posts of the thread that contains post `ID`")
		fs.BoolVar(&o.peek, "peek", false, "don't mark them seen")
		// 9m by default: less than the 10 minutes that harnesses such as
		// Claude Code give a command at most.
		fs.StringVar(&o.timeout, "timeout", "9m", "give up after `DURATION`, such as 30s or 1h, exiting with status 3; 0 waits without limit")
	},
}

// parseTimeout parses --timeout, where 0 is no limit.
func parseTimeout(s string) (time.Duration, error) {
	if s == "0" {
		return 0, nil
	}
	d, err := post.ParseSince(s)
	if err != nil {
		return 0, usageError{"--timeout: " + err.Error()}
	}
	return d, nil
}

func runWait(c *ctx, _ []string) error {
	limit, err := parseTimeout(c.o.timeout)
	if err != nil {
		return err
	}
	req := post.UnreadRequest{Peek: c.o.peek, ToMe: c.o.toMe}
	if c.o.set["thread"] {
		if req.Thread, err = c.id(c.o.thread); err != nil {
			return err
		}
	}
	cl, err := c.client(true)
	if err != nil {
		return err
	}
	now := c.env.Now
	if now == nil {
		now = time.Now
	}
	deadline := now().Add(limit)
	for {
		hold := post.MaxWait
		if limit > 0 {
			hold = min(hold, deadline.Sub(now()))
		}
		held := req
		// Whole seconds, at least one.
		held.Wait = max(int64((hold+time.Second-1)/time.Second), 1)
		u, err := cl.Unread(c.ctx(), held)
		if err != nil {
			return err
		}
		if len(u.Posts)+len(u.Edited) > 0 && !req.Peek {
			// The request that waited marked nothing; this one marks what it
			// shows, unless another request of the reader's just did.
			if u, err = cl.Unread(c.ctx(), req); err != nil {
				return err
			}
		}
		if len(u.Posts)+len(u.Edited) > 0 {
			return c.printUnread(u, cl)
		}
		if limit > 0 && !now().Before(deadline) {
			if err := c.printUnread(u, cl); err != nil {
				return err
			}
			return statusError{status: 3, msg: fmt.Sprintf("nothing to show within %s", c.o.timeout)}
		}
	}
}
