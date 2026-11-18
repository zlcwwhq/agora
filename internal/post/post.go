package post

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Version is the format version that this package reads and writes.
const Version = 1

// Post is one post: a root, which starts a thread and has a title, or a reply,
// which has a thread and the post it replies to.
type Post struct {
	ID      ID
	Harness string
	Session string
	// Created is the creation time as written, in RFC 3339 with an offset.
	Created string
	Title   string
	Thread  ID
	ReplyTo ID
	Tags    []string
	Body    string
}

// IsRoot reports whether p starts a thread.
func (p Post) IsRoot() bool { return p.Thread.IsZero() }

// ThreadID returns the id of p's thread: its own for a root.
func (p Post) ThreadID() ID {
	if p.IsRoot() {
		return p.ID
	}
	return p.Thread
}

// CreatedTime parses Created.
func (p Post) CreatedTime() (time.Time, error) { return time.Parse(time.RFC3339, p.Created) }

// Validate checks everything that Render needs and Parse checks.
func (p Post) Validate() error {
	if !ValidNode(p.ID.Node) || p.ID.N < 1 {
		return fmt.Errorf("invalid id %q", p.ID)
	}
	if !ValidHarness(p.Harness) {
		return fmt.Errorf("invalid harness %q", p.Harness)
	}
	if !ValidSession(p.Session) {
		return fmt.Errorf("invalid session %q", p.Session)
	}
	if _, err := p.CreatedTime(); err != nil {
		return fmt.Errorf("invalid created time %q: want RFC 3339", p.Created)
	}
	if p.Thread.IsZero() != p.ReplyTo.IsZero() {
		return errors.New("a reply needs both thread and reply-to, a root neither")
	}
	if p.IsRoot() {
		if strings.TrimSpace(p.Title) == "" {
			return errors.New("a root needs a title")
		}
		if strings.ContainsAny(p.Title, "\r\n") {
			return errors.New("a title is a single line")
		}
	} else {
		if p.Title != "" {
			return errors.New("a reply has no title")
		}
		for _, id := range []ID{p.Thread, p.ReplyTo} {
			if !ValidNode(id.Node) || id.N < 1 {
				return fmt.Errorf("invalid id %q", id)
			}
		}
		if p.Thread == p.ID || p.ReplyTo == p.ID {
			return errors.New("a reply can't be its own thread or reply to itself")
		}
	}
	for _, tag := range p.Tags {
		if t, ok := NormalizeTag(tag); !ok || t != tag {
			return fmt.Errorf("tag %q isn't normalized", tag)
		}
	}
	return nil
}

// Render returns the file contents of p, which must be valid.
func Render(p Post) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	line := func(key string, value any) {
		var v bytes.Buffer
		e := json.NewEncoder(&v)
		e.SetEscapeHTML(false)
		// Encoding strings, slices of strings, and integers can't fail.
		_ = e.Encode(value)
		b.WriteString(key)
		b.WriteString(": ")
		b.Write(v.Bytes()) // Encode ends the value with a newline.
	}
	line("v", Version)
	line("id", p.ID.String())
	line("harness", p.Harness)
	line("session", p.Session)
	line("created", p.Created)
	if p.IsRoot() {
		line("title", p.Title)
	} else {
		line("thread", p.Thread.String())
		line("reply-to", p.ReplyTo.String())
	}
	if len(p.Tags) > 0 {
		line("tags", p.Tags)
	}
	b.WriteString("---\n")
	b.WriteString(p.Body)
	if p.Body != "" && !strings.HasSuffix(p.Body, "\n") {
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Parse reads a post file. Unknown keys are ignored, and tags that aren't
// valid are dropped.
func Parse(data []byte) (Post, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return Post{}, errors.New("missing header: the file must start with a line of ---")
	}
	header, body, ok := cutHeader(rest)
	if !ok {
		return Post{}, errors.New("unterminated header: missing the closing line of ---")
	}

	values := map[string]json.RawMessage{}
	var lines []string
	if header != "" {
		lines = strings.Split(header, "\n")
	}
	for i, l := range lines {
		key, value, ok := strings.Cut(l, ": ")
		if !ok || !keyPattern.MatchString(key) {
			return Post{}, fmt.Errorf("header line %d: want <key>: <JSON value>", i+2)
		}
		if _, dup := values[key]; dup {
			return Post{}, fmt.Errorf("header line %d: duplicate key %q", i+2, key)
		}
		values[key] = json.RawMessage(value)
	}

	var version int
	if err := decode(values, "v", &version, true); err != nil {
		return Post{}, err
	}
	if version != Version {
		return Post{}, fmt.Errorf("unsupported format version %d", version)
	}

	var p Post
	var id, thread, replyTo string
	var tags []string
	for _, f := range []struct {
		key      string
		target   any
		required bool
	}{
		{"id", &id, true},
		{"harness", &p.Harness, true},
		{"session", &p.Session, true},
		{"created", &p.Created, true},
		{"title", &p.Title, false},
		{"thread", &thread, false},
		{"reply-to", &replyTo, false},
		{"tags", &tags, false},
	} {
		if err := decode(values, f.key, f.target, f.required); err != nil {
			return Post{}, err
		}
	}

	var err error
	if p.ID, err = ParseID(id); err != nil {
		return Post{}, err
	}
	if thread != "" {
		if p.Thread, err = ParseID(thread); err != nil {
			return Post{}, fmt.Errorf("thread: %w", err)
		}
	}
	if replyTo != "" {
		if p.ReplyTo, err = ParseID(replyTo); err != nil {
			return Post{}, fmt.Errorf("reply-to: %w", err)
		}
	}
	p.Tags = []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		if t, ok := NormalizeTag(tag); ok && !seen[t] {
			seen[t] = true
			p.Tags = append(p.Tags, t)
		}
	}
	p.Body = body
	if err := p.Validate(); err != nil {
		return Post{}, err
	}
	return p, nil
}

// cutHeader splits the text after the opening line into the header lines and
// the body, at the first line of exactly ---.
func cutHeader(rest string) (header, body string, ok bool) {
	if b, ok := strings.CutPrefix(rest, "---\n"); ok {
		return "", b, true
	}
	if rest == "---" {
		return "", "", true
	}
	if i := strings.Index(rest, "\n---\n"); i >= 0 {
		return rest[:i], rest[i+len("\n---\n"):], true
	}
	if h, ok := strings.CutSuffix(rest, "\n---"); ok {
		return h, "", true
	}
	return "", "", false
}

func decode(values map[string]json.RawMessage, key string, target any, required bool) error {
	raw, ok := values[key]
	if !ok {
		if required {
			return fmt.Errorf("missing required key %q", key)
		}
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("key %q: %w", key, err)
	}
	return nil
}
