package post

import (
	"slices"
	"unicode"
	"unicode/utf8"
)

// Reaction is a reaction to a post and the authors that gave it, displayed,
// in the order in which they gave it.
type Reaction struct {
	Reaction string   `json:"reaction"`
	Authors  []string `json:"authors"`
}

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

// CheckReaction returns an error of kind Invalid unless s is a reaction: 1 to
// 32 characters without whitespace or control characters, kept as given.
func CheckReaction(s string) error {
	ok := utf8.ValidString(s) && utf8.RuneCountInString(s) >= 1 && utf8.RuneCountInString(s) <= 32
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			ok = false
		}
	}
	if !ok {
		return Errorf(Invalid, "invalid reaction %q: reactions have 1 to 32 characters, without whitespace or control characters, such as +1, ack, or 👀", s)
	}
	return nil
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
