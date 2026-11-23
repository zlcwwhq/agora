package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/zlcwwhq/agora/internal/post"
)

func singleTagFlags(fs *flag.FlagSet, o *opts) {
	fs.Var(&o.addTags, "add-tag", "a `tag` to add, after the post's others, repeatable")
	fs.Var(&o.rmTags, "remove-tag", "a `tag` to remove, repeatable")
}

// singleTags sets the single tags that edit's --add-tag and --remove-tag
// change.
func (c *ctx) singleTags(e *post.Edit) error {
	add, err := post.Tags(c.o.addTags)
	if err != nil {
		return usageError{err.Error()}
	}
	remove, err := post.Tags(c.o.rmTags)
	if err != nil {
		return usageError{err.Error()}
	}
	if len(add) > 0 {
		e.AddTags = &add
	}
	if len(remove) > 0 {
		e.RemoveTags = &remove
	}
	return nil
}

func runHistory(c *ctx, args []string) error {
	id, err := c.id(args[0])
	if err != nil {
		return err
	}
	cl, err := c.client(false)
	if err != nil {
		return err
	}
	h, err := cl.History(c.ctx(), id)
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(h)
	}
	c.printf("#%d in thread #%d: %s, oldest first\n\n", h.ID, h.Thread, plural(len(h.Versions), "version", "versions"))
	for _, v := range h.Versions {
		parts := append(append([]string{fmt.Sprintf("version %d", v.Version), v.Author}, via(v.Carrier, v.Model)...), when(v.Time))
		if len(v.Tags) > 0 {
			parts = append(parts, hashTags(v.Tags))
		}
		c.printf("── %s\n", strings.Join(parts, " · "))
		if v.Title != "" {
			c.printf("%s\n", v.Title)
		}
		c.printf("%s\n", v.Body)
	}
	return nil
}

func runReact(c *ctx, args []string) error {
	id, err := c.id(args[0])
	if err != nil {
		return err
	}
	reaction := args[1]
	if err := post.CheckReaction(reaction); err != nil {
		return usageError{err.Error()}
	}
	cl, err := c.client(true)
	if err != nil {
		return err
	}
	r, err := cl.React(c.ctx(), id, post.React{Reaction: reaction, Undo: c.o.undo})
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(r)
	}
	verb := "reacted with"
	if c.o.undo {
		verb = "took back"
	}
	c.printf("%s %s on #%d  %s\n", verb, reaction, r.ID, c.postURL(r.Thread, r.ID))
	c.printReactions(r.Reactions)
	return nil
}

// printReactions prints a post's reactions on a line, if it has any.
func (c *ctx) printReactions(rs []post.Reaction) {
	if len(rs) == 0 {
		return
	}
	var parts []string
	for _, r := range rs {
		parts = append(parts, fmt.Sprintf("%s ×%d (%s)", r.Reaction, len(r.Authors), strings.Join(r.Authors, ", ")))
	}
	c.printf("reactions: %s\n", strings.Join(parts, " · "))
}

// forMe adds to an unread post's context that it mentions the reader who.
func forMe(context string, p post.Post, who post.Identity) string {
	for _, m := range p.Mentions {
		harness, session, _ := strings.Cut(m, "/")
		if (post.Mention{Harness: harness, Session: session}).Names(who.Harness, who.Session) {
			return context + " · mentions you"
		}
	}
	return context
}

// printEdited prints the posts that unread reports edited since the reader
// saw them.
func (c *ctx) printEdited(ps []post.Post, who post.Identity) {
	if len(ps) == 0 {
		return
	}
	c.printf("edited since you saw them:\n\n")
	for _, p := range ps {
		context := fmt.Sprintf("version %d: agora history %d", p.Versions, p.ID)
		if !p.IsRoot() {
			context = fmt.Sprintf("in thread #%d · %s", p.Thread, context)
		}
		c.printPost(p, false, forMe(context, p, who))
	}
}
