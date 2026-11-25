package cli

import (
	"fmt"
	"strings"

	"github.com/zlcwwhq/agora/internal/post"
)

func runVote(c *ctx, args []string) error {
	usage := usagef("usage: agora vote ID (up | down | --undo)")
	var v string
	switch {
	case c.o.undo && len(args) == 1:
		v = post.VoteNone
	case !c.o.undo && len(args) == 2 && (args[1] == post.VoteUp || args[1] == post.VoteDown):
		v = args[1]
	default:
		return usage
	}
	id, err := c.id(args[0])
	if err != nil {
		return err
	}
	cl, err := c.client(true)
	if err != nil {
		return err
	}
	r, err := cl.Vote(c.ctx(), id, post.Vote{Vote: v})
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(r)
	}
	if v == post.VoteNone {
		c.printf("took back your vote on #%d  %s\n", r.ID, c.postURL(r.Thread, r.ID))
	} else {
		c.printf("voted #%d %s  %s\n", r.ID, v, c.postURL(r.Thread, r.ID))
	}
	c.printf("%s\n", votesLine(r.Score, r.Votes))
	return nil
}

// votesLine describes a post's score and who voted it each way.
func votesLine(score int, v post.Votes) string {
	parts := []string{fmt.Sprintf("score %d", score)}
	if len(v.Up) > 0 {
		parts = append(parts, fmt.Sprintf("up ×%d (%s)", len(v.Up), strings.Join(v.Up, ", ")))
	}
	if len(v.Down) > 0 {
		parts = append(parts, fmt.Sprintf("down ×%d (%s)", len(v.Down), strings.Join(v.Down, ", ")))
	}
	return "votes: " + strings.Join(parts, " · ")
}

// printVotes prints a post's votes on a line, if it has any.
func (c *ctx) printVotes(p post.Post) {
	if len(p.Votes.Up)+len(p.Votes.Down) > 0 {
		c.printf("%s\n", votesLine(p.Score, p.Votes))
	}
}
