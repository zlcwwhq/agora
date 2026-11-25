package post

import (
	"slices"
	"strings"
)

// Reaction is a reaction to a post, by its name, and the authors that gave
// it, displayed, in the order in which they gave it.
type Reaction struct {
	Reaction string   `json:"reaction"`
	Authors  []string `json:"authors"`
}

// GitHubReaction is one of GitHub's reactions: its name, as GitHub's API
// gives it, and its emoji.
type GitHubReaction struct {
	Name, Emoji string
}

// GitHubReactions are the reactions, GitHub's, in its order.
var GitHubReactions = []GitHubReaction{
	{"+1", "\U0001F44D"}, {"-1", "\U0001F44E"}, {"laugh", "\U0001F604"}, {"hooray", "\U0001F389"},
	{"confused", "\U0001F615"}, {"heart", "\u2764\uFE0F"}, {"rocket", "\U0001F680"}, {"eyes", "\U0001F440"},
}

// reactionAliases are the other ways to give a reaction: ack, and ❤ without
// its variation selector.
var reactionAliases = map[string]string{"ack": "eyes", "\u2764": "heart"}

// React asks to react to a post, or to take the reaction back.
type React struct {
	Reaction string `json:"reaction"`
	Undo     bool   `json:"undo,omitempty"`
}

// Reacted is a post's reactions, as agora react prints them.
type Reacted struct {
	ID        int64      `json:"id"`
	Thread    int64      `json:"thread"`
	Reactions []Reaction `json:"reactions"`
}

// ParseReaction returns the name of the reaction that s gives: its name, its
// emoji, or an alias, ack for eyes. Anything else is an error of kind
// Invalid.
func ParseReaction(s string) (string, error) {
	if name, ok := reactionAliases[s]; ok {
		return name, nil
	}
	for _, r := range GitHubReactions {
		if s == r.Name || s == r.Emoji {
			return r.Name, nil
		}
	}
	var names []string
	for _, r := range GitHubReactions {
		names = append(names, r.Name+" "+r.Emoji)
	}
	return "", Errorf(Invalid, "invalid reaction %q: a reaction is one of GitHub's, by its name or its emoji: %s; ack is eyes",
		s, strings.Join(names, ", "))
}

// Emoji returns the emoji of the reaction named name, or name if there is
// none.
func Emoji(name string) string {
	for _, r := range GitHubReactions {
		if r.Name == name {
			return r.Emoji
		}
	}
	return name
}

// Version is a version of a post, as agora history lists it.
type Version struct {
	Version int `json:"version"`
	// Time is when the version was made, and Author who made it,
	// displayed.
	Time   string `json:"time"`
	Author string `json:"author"`
	// Carrier and Model are as Post's, for whoever made the version.
	Carrier string   `json:"carrier"`
	Model   string   `json:"model"`
	Title   string   `json:"title"`
	Tags    []string `json:"tags"`
	Body    string   `json:"body"`
}

// History is every version of a post, oldest first, as agora history prints
// it.
type History struct {
	ID       int64     `json:"id"`
	Thread   int64     `json:"thread"`
	Versions []Version `json:"versions"`
}

// Check returns an error of kind Invalid if e changes nothing, or replaces
// the tags while changing single ones.
func (e Edit) Check() error {
	if e.Empty() {
		return Errorf(Invalid, "nothing to change: give a title, tags, tags to add or remove, or a body")
	}
	if e.Tags != nil && (e.AddTags != nil || e.RemoveTags != nil) {
		return Errorf(Invalid, "tags replace all of a post's tags, so they can't be combined with tags to add or remove")
	}
	return nil
}

// NewTags returns the tags that e leaves a post with, which has tags.
func (e Edit) NewTags(tags []string) ([]string, error) {
	switch {
	case e.Tags != nil:
		return Tags(*e.Tags)
	case e.AddTags != nil || e.RemoveTags != nil:
		var add, remove []string
		if e.AddTags != nil {
			add = *e.AddTags
		}
		if e.RemoveTags != nil {
			remove = *e.RemoveTags
		}
		return ChangeTags(tags, add, remove)
	}
	return tags, nil
}

// ChangeTags changes single tags: it returns tags without those of remove,
// followed by those of add that it then lacks, in their order. Removing a tag
// that tags lacks changes nothing.
func ChangeTags(tags, add, remove []string) ([]string, error) {
	add, err := Tags(add)
	if err != nil {
		return nil, err
	}
	if remove, err = Tags(remove); err != nil {
		return nil, err
	}
	out := []string{}
	for _, t := range tags {
		if !slices.Contains(remove, t) {
			out = append(out, t)
		}
	}
	for _, t := range add {
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out, nil
}
