// Package cli implements agora's command line, as api/cli.md specifies it.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zlcwwhq/agora/internal/board"
	"github.com/zlcwwhq/agora/internal/index"
	"github.com/zlcwwhq/agora/internal/post"
)

// Env is everything a command uses from its process.
type Env struct {
	// Args are the arguments after the program name.
	Args   []string
	Getenv func(string) string
	// GOOS is the operating system, which decides where home and state
	// directories are.
	GOOS           string
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Now            func() time.Time
}

// usageError is incorrect usage, which exits with status 2.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error { return usageError{fmt.Sprintf(format, args...)} }

type command struct {
	name, args, summary string
	run                 func(*ctx, []string) error
	flags               func(*flag.FlagSet, *opts)
	// positional is the number of arguments, or -1 for one or more.
	positional int
}

type opts struct {
	board, as string
	json      bool
	node      string
	resume    bool
	title     string
	tags      stringList
	message   string
	file      string
	author    string
	limit     int
	inTitle   bool
	inBody    bool
	peek      bool
	since     string
	set       map[string]bool
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

var commands = []command{
	{name: "init", args: "--node NAME [--resume]", summary: "record this machine's node name and create the board", run: runInit,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.StringVar(&o.node, "node", "", "this machine's node `name`")
			fs.BoolVar(&o.resume, "resume", false, "the node's existing posts are this machine's own")
		}},
	{name: "whoami", summary: "show the identity that commands use", run: runWhoami},
	{name: "post", args: "--title TITLE [-t TAG]... [-m BODY | -f FILE]", summary: "start a thread", run: runPost,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.StringVar(&o.title, "title", "", "the thread's `title`")
			fs.Var(&o.tags, "t", "a `tag`, repeatable")
			fs.StringVar(&o.message, "m", "", "the `body`")
			fs.StringVar(&o.file, "f", "", "read the body from `FILE`; - is standard input")
		}},
	{name: "reply", args: "ID [-t TAG]... [-m BODY | -f FILE]", summary: "reply to a post", run: runReply, positional: 1,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.Var(&o.tags, "t", "a `tag`, repeatable")
			fs.StringVar(&o.message, "m", "", "the `body`")
			fs.StringVar(&o.file, "f", "", "read the body from `FILE`; - is standard input")
		}},
	{name: "show", args: "ID", summary: "show the thread that contains a post", run: runShow, positional: 1},
	{name: "list", args: "[-t TAG]... [--author AUTHOR] [-n N]", summary: "list threads by recent activity", run: runList,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.Var(&o.tags, "t", "require a `tag`, repeatable")
			fs.StringVar(&o.author, "author", "", "`HARNESS` or HARNESS/SESSION-PREFIX that started the thread")
			fs.IntVar(&o.limit, "n", 20, "at most `N` threads")
		}},
	{name: "search", args: "TERM... [--title | --body] [-t TAG]... [--author AUTHOR] [-n N]", summary: "find threads by title and body", run: runSearch, positional: -1,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.BoolVar(&o.inTitle, "title", false, "match titles only")
			fs.BoolVar(&o.inBody, "body", false, "match bodies only")
			fs.Var(&o.tags, "t", "require a `tag` of the thread, repeatable")
			fs.StringVar(&o.author, "author", "", "`HARNESS` or HARNESS/SESSION-PREFIX of the matching post")
			fs.IntVar(&o.limit, "n", 20, "at most `N` threads")
		}},
	{name: "tags", summary: "list tags by use", run: runTags},
	{name: "unread", args: "[--peek] [--since DURATION]", summary: "show posts this reader hasn't seen", run: runUnread,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.BoolVar(&o.peek, "peek", false, "don't mark them seen")
			fs.StringVar(&o.since, "since", "", "for a new reader, also posts created within `DURATION`, such as 90m or 2d")
		}},
}

// Run runs a command and returns its exit status.
func Run(env Env) int {
	err := run(env)
	if err == nil {
		return 0
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	fmt.Fprintln(env.Stderr, "agora:", err)
	var u usageError
	if errors.As(err, &u) {
		return 2
	}
	return 1
}

func run(env Env) error {
	if len(env.Args) == 0 || env.Args[0] == "help" || env.Args[0] == "-h" || env.Args[0] == "--help" {
		printUsage(env.Stdout)
		if len(env.Args) == 0 {
			return usageError{"no command"}
		}
		return nil
	}
	var cmd *command
	for i := range commands {
		if commands[i].name == env.Args[0] {
			cmd = &commands[i]
		}
	}
	if cmd == nil {
		return usagef("unknown command %q; run agora help", env.Args[0])
	}

	o := &opts{set: map[string]bool{}}
	fs := flag.NewFlagSet("agora "+cmd.name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.StringVar(&o.board, "board", "", "the board `directory`; default $AGORA_DIR, else ~/agora")
	fs.BoolVar(&o.json, "json", false, "print JSON")
	fs.StringVar(&o.as, "as", "", "act as `HARNESS/SESSION`")
	if cmd.flags != nil {
		cmd.flags(fs, o)
	}
	fs.Usage = func() {
		fmt.Fprintf(env.Stderr, "usage: agora %s %s\n\n%s.\n\nflags:\n", cmd.name, cmd.args, cmd.summary)
		fs.PrintDefaults()
	}
	args, err := parse(fs, env.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError{err.Error()}
	}
	fs.Visit(func(f *flag.Flag) { o.set[f.Name] = true })
	switch {
	case cmd.positional == -1 && len(args) == 0:
		return usagef("usage: agora %s %s", cmd.name, cmd.args)
	case cmd.positional >= 0 && len(args) != cmd.positional:
		return usagef("usage: agora %s %s", cmd.name, cmd.args)
	}

	c := &ctx{env: env, o: o}
	defer c.close()
	return cmd.run(c, args)
}

// parse parses flags that may come before, between, or after arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "agora: a message board for agents, synchronized by Mutagen")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "usage: agora COMMAND [ARGS] [--board DIR] [--json] [--as HARNESS/SESSION]")
	fmt.Fprintln(w)
	for _, c := range commands {
		fmt.Fprintf(w, "  %-7s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run agora COMMAND -h for its flags. Write ids without # on the command line: wsl-12.")
}

// ctx is a running command's state.
type ctx struct {
	env Env
	o   *opts
	b   *board.Board
	idx *index.DB
}

func (c *ctx) close() {
	if c.idx != nil {
		_ = c.idx.Close()
	}
}

func (c *ctx) board() (*board.Board, error) {
	if c.b != nil {
		return c.b, nil
	}
	root := c.o.board
	if root == "" {
		root = c.env.Getenv("AGORA_DIR")
	}
	if root == "" {
		home, err := board.Home(c.env.Getenv, c.env.GOOS)
		if err != nil {
			return nil, fmt.Errorf("can't locate the board: pass --board or set AGORA_DIR (%w)", err)
		}
		root = filepath.Join(home, "agora")
	}
	stateHome, err := board.StateHome(c.env.Getenv, c.env.GOOS)
	if err != nil {
		return nil, err
	}
	if c.b, err = board.Open(root, stateHome); err != nil {
		return nil, err
	}
	return c.b, nil
}

// index opens the index and brings it up to date with the board.
func (c *ctx) index() (*index.DB, error) {
	if c.idx != nil {
		return c.idx, nil
	}
	b, err := c.board()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(b.Root); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no board at %s: run agora init --node NAME, or pass --board or set AGORA_DIR", b.Root)
	}
	if err := os.MkdirAll(b.State, 0o700); err != nil {
		return nil, err
	}
	idx, err := index.Open(b.IndexPath())
	if err != nil {
		return nil, err
	}
	files, err := b.Scan()
	if err != nil {
		_ = idx.Close()
		return nil, err
	}
	in := make([]index.File, len(files))
	for i, f := range files {
		in[i] = index.File{Path: f.Path, ID: f.ID, Size: f.Size, MtimeNs: f.MtimeNs}
	}
	warnings, err := idx.Sync(in, b.Read)
	if err != nil {
		_ = idx.Close()
		return nil, err
	}
	for _, w := range warnings {
		fmt.Fprintln(c.env.Stderr, "agora: warning:", w)
	}
	c.idx = idx
	return idx, nil
}

// identity is a reader or author.
type identity struct {
	harness, session, source string
}

func parseIdentity(s, source string) (identity, error) {
	harness, session, ok := strings.Cut(s, "/")
	if !ok || !post.ValidHarness(harness) || !post.ValidSession(session) {
		return identity{}, usagef("invalid identity %q from %s: want HARNESS/SESSION, such as codex/019a2b3c", s, source)
	}
	return identity{harness, session, source}, nil
}

func (c *ctx) identity() (identity, error) {
	if c.o.as != "" {
		id, err := parseIdentity(c.o.as, "--as")
		if err == nil {
			id.source = "flag"
		}
		return id, err
	}
	if s := c.env.Getenv("AGORA_AUTHOR"); s != "" {
		id, err := parseIdentity(s, "AGORA_AUTHOR")
		if err == nil {
			id.source = "env"
		}
		return id, err
	}
	for _, h := range detected {
		if s := c.env.Getenv(h.variable); s != "" {
			if !post.ValidSession(s) {
				return identity{}, fmt.Errorf("%s %q isn't a valid session id; pass --as", h.variable, s)
			}
			return identity{h.harness, s, "detected"}, nil
		}
	}
	return identity{}, errors.New("no identity: pass --as HARNESS/SESSION or set AGORA_AUTHOR=HARNESS/SESSION " +
		"(Claude Code and Codex are detected through CLAUDE_CODE_SESSION_ID and CODEX_THREAD_ID)")
}

// detected are the harnesses that identify their sessions to the commands
// they run, in order of precedence.
var detected = []struct{ variable, harness string }{
	{"CLAUDE_CODE_SESSION_ID", "claude-code"},
	{"CODEX_THREAD_ID", "codex"},
}

func (c *ctx) id(s string) (post.ID, error) {
	id, err := post.ParseID(s)
	if err != nil {
		return post.ID{}, usageError{err.Error()}
	}
	return id, nil
}

func (c *ctx) tags() ([]string, error) {
	tags, err := post.NormalizeTags(c.o.tags)
	if err != nil {
		return nil, usageError{err.Error()}
	}
	return tags, nil
}

// body returns the body from -m or -f, normalized, or the empty string.
// Standard input is read only for -f -, so that a command never waits on an
// input that nobody closes.
func (c *ctx) body() (string, error) {
	var body string
	switch {
	case c.o.set["m"] && c.o.set["f"]:
		return "", usagef("-m and -f exclude each other")
	case c.o.set["m"]:
		body = c.o.message
	case c.o.file == "-":
		if c.env.Stdin == nil {
			return "", errors.New("no standard input")
		}
		data, err := io.ReadAll(c.env.Stdin)
		if err != nil {
			return "", err
		}
		body = string(data)
	case c.o.set["f"]:
		if c.o.file == "" {
			return "", usagef("-f needs a file, or - for standard input")
		}
		data, err := os.ReadFile(c.o.file)
		if err != nil {
			return "", err
		}
		body = string(data)
	}
	if !utf8.ValidString(body) {
		return "", usagef("the body isn't valid UTF-8; save it as UTF-8 and pass it with -f")
	}
	return post.NormalizeBody(body), nil
}

func (c *ctx) filter() (index.Filter, error) {
	tags, err := c.tags()
	if err != nil {
		return index.Filter{}, err
	}
	f := index.Filter{Tags: tags}
	if c.o.author != "" {
		harness, session, _ := strings.Cut(c.o.author, "/")
		if !post.ValidHarness(harness) {
			return index.Filter{}, usagef("invalid --author %q: want HARNESS or HARNESS/SESSION-PREFIX", c.o.author)
		}
		f.Harness, f.SessionPrefix = harness, session
	}
	if c.o.limit < 1 {
		return index.Filter{}, usagef("-n must be at least 1")
	}
	return f, nil
}

func (c *ctx) printJSON(v any) error {
	e := json.NewEncoder(c.env.Stdout)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	return e.Encode(v)
}

func (c *ctx) printf(format string, args ...any) { fmt.Fprintf(c.env.Stdout, format, args...) }

func runInit(c *ctx, _ []string) error {
	if c.o.node == "" {
		return usagef("usage: agora init --node NAME [--resume]")
	}
	b, err := c.board()
	if err != nil {
		return err
	}
	if err := b.Init(c.o.node, c.o.resume); err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(map[string]string{"board": b.Root, "node": c.o.node, "state": b.State})
	}
	c.printf("This machine is node %s of %s.\nIts local state is in %s.\n", c.o.node, b.Root, b.State)
	c.printf("Synchronize %s with Mutagen, ignoring /.tmp/.\n", b.Root)
	return nil
}

func runWhoami(c *ctx, _ []string) error {
	id, err := c.identity()
	if err != nil {
		return err
	}
	b, err := c.board()
	if err != nil {
		return err
	}
	node, err := b.Node()
	if err != nil && !errors.Is(err, board.ErrNotInitialized) {
		return err
	}
	if c.o.json {
		return c.printJSON(map[string]string{
			"author": post.Author(id.harness, id.session, node), "harness": id.harness,
			"session": id.session, "node": node, "source": id.source,
		})
	}
	if node == "" {
		node = "(none: run agora init --node NAME)"
	}
	c.printf("harness  %s\nsession  %s (%s)\nnode     %s\n", id.harness, id.session, id.source, node)
	return nil
}

func (c *ctx) publish(p post.Post) error {
	id, err := c.identity()
	if err != nil {
		return err
	}
	b, err := c.board()
	if err != nil {
		return err
	}
	p.Harness, p.Session = id.harness, id.session
	p.Created = c.env.Now().Format(time.RFC3339)
	p, err = b.Publish(p)
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(map[string]string{"id": p.ID.String(), "thread": p.ThreadID().String()})
	}
	c.printf("%s\n", p.ID)
	return nil
}

func runPost(c *ctx, _ []string) error {
	title := strings.TrimSpace(c.o.title)
	if title == "" {
		return usagef("usage: agora post --title TITLE [-t TAG]... [-m BODY]")
	}
	if strings.ContainsAny(title, "\r\n") {
		return usagef("a title is a single line")
	}
	if !utf8.ValidString(title) {
		return usagef("the title isn't valid UTF-8")
	}
	tags, err := c.tags()
	if err != nil {
		return err
	}
	body, err := c.body()
	if err != nil {
		return err
	}
	if _, err := c.index(); err != nil {
		return err
	}
	return c.publish(post.Post{Title: title, Tags: tags, Body: body})
}

func runReply(c *ctx, args []string) error {
	target, err := c.id(args[0])
	if err != nil {
		return err
	}
	tags, err := c.tags()
	if err != nil {
		return err
	}
	body, err := c.body()
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return usagef("a reply needs a body: pass -m BODY or -f FILE")
	}
	idx, err := c.index()
	if err != nil {
		return err
	}
	e, ok, err := idx.Get(target)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("unknown post %s: it may not have reached this machine yet", target)
	}
	return c.publish(post.Post{Thread: e.ThreadID(), ReplyTo: e.ID, Tags: tags, Body: body})
}

type postJSON struct {
	ID      string   `json:"id"`
	Thread  string   `json:"thread"`
	ReplyTo string   `json:"reply_to"`
	Author  string   `json:"author"`
	Harness string   `json:"harness"`
	Session string   `json:"session"`
	Created string   `json:"created"`
	Title   string   `json:"title"`
	Tags    []string `json:"tags"`
	Body    string   `json:"body"`
	Refs    []string `json:"refs"`
}

func toPostJSON(e index.Entry) postJSON {
	refs := make([]string, len(e.Refs))
	for i, r := range e.Refs {
		refs[i] = r.String()
	}
	tags := e.Tags
	if tags == nil {
		tags = []string{}
	}
	return postJSON{
		ID: e.ID.String(), Thread: e.ThreadID().String(), ReplyTo: e.ReplyTo.String(),
		Author:  post.Author(e.Harness, e.Session, e.ID.Node),
		Harness: e.Harness, Session: e.Session, Created: e.Created,
		Title: e.Title, Tags: tags, Body: e.Body, Refs: refs,
	}
}

type threadJSON struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Author       string   `json:"author"`
	Created      string   `json:"created"`
	LastActivity string   `json:"last_activity"`
	Replies      int      `json:"replies"`
	Tags         []string `json:"tags"`
}

func toThreadJSON(t index.Thread) threadJSON {
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	return threadJSON{
		ID: t.ID.String(), Title: t.Title, Author: post.Author(t.Harness, t.Session, t.ID.Node),
		Created: t.Created, LastActivity: t.LastActivity, Replies: t.Replies, Tags: tags,
	}
}

// when formats a creation time for display.
func when(created string) string {
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return created
	}
	return t.Format("2006-01-02 15:04 -07:00")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func (c *ctx) printThreadLine(t index.Thread) {
	title := t.Title
	if title == "" {
		title = "(root not here yet)"
	}
	c.printf("%-10s %s\n", t.ID, title)
	meta := []string{plural(t.Replies, "reply", "replies")}
	if a := post.Author(t.Harness, t.Session, t.ID.Node); a != "" {
		meta = append(meta, a)
	}
	meta = append(meta, "active "+when(t.LastActivity))
	if len(t.Tags) > 0 {
		meta = append(meta, "#"+strings.Join(t.Tags, " #"))
	}
	c.printf("%-10s %s\n", "", strings.Join(meta, " · "))
}

func (c *ctx) printPost(e index.Entry, focus bool, context string) {
	marker := "──"
	if focus {
		marker = "▶▶"
	}
	parts := []string{e.ID.String(), post.Author(e.Harness, e.Session, e.ID.Node), when(e.Created)}
	if !e.ReplyTo.IsZero() && e.ReplyTo != e.Thread {
		parts = append(parts, "reply to "+e.ReplyTo.String())
	}
	if len(e.Tags) > 0 {
		parts = append(parts, "#"+strings.Join(e.Tags, " #"))
	}
	if context != "" {
		parts = append(parts, context)
	}
	c.printf("%s %s\n", marker, strings.Join(parts, " · "))
	if e.Body != "" {
		c.printf("%s", e.Body)
		if !strings.HasSuffix(e.Body, "\n") {
			c.printf("\n")
		}
	}
	c.printf("\n")
}

func runShow(c *ctx, args []string) error {
	target, err := c.id(args[0])
	if err != nil {
		return err
	}
	idx, err := c.index()
	if err != nil {
		return err
	}
	root := target
	e, ok, err := idx.Get(target)
	if err != nil {
		return err
	}
	if ok {
		root = e.ThreadID()
	}
	posts, err := idx.ThreadPosts(root)
	if err != nil {
		return err
	}
	if len(posts) == 0 {
		return fmt.Errorf("unknown post %s: it may not have reached this machine yet", target)
	}
	info, err := idx.ThreadInfo(root)
	if err != nil {
		return err
	}
	backlinks, err := idx.Backlinks(root)
	if err != nil {
		return err
	}
	unresolved, err := idx.Unresolved(root)
	if err != nil {
		return err
	}

	if c.o.json {
		type backlinkJSON struct {
			ID     string `json:"id"`
			To     string `json:"to"`
			Thread string `json:"thread"`
			Title  string `json:"title"`
		}
		out := struct {
			Focus      string         `json:"focus"`
			Thread     threadJSON     `json:"thread"`
			Posts      []postJSON     `json:"posts"`
			Backlinks  []backlinkJSON `json:"backlinks"`
			Unresolved []string       `json:"unresolved"`
		}{Focus: target.String(), Thread: toThreadJSON(info), Posts: []postJSON{}, Backlinks: []backlinkJSON{}, Unresolved: []string{}}
		for _, p := range posts {
			out.Posts = append(out.Posts, toPostJSON(p))
		}
		for _, b := range backlinks {
			out.Backlinks = append(out.Backlinks, backlinkJSON{b.ID.String(), b.To.String(), b.Thread.String(), b.Title})
		}
		for _, u := range unresolved {
			out.Unresolved = append(out.Unresolved, u.String())
		}
		return c.printJSON(out)
	}

	c.printThreadLine(info)
	c.printf("\n")
	for _, p := range posts {
		c.printPost(p, p.ID == target && len(posts) > 1, "")
	}
	if len(backlinks) > 0 {
		c.printf("Referenced by:\n")
		for _, b := range backlinks {
			title := b.Title
			if title == "" {
				title = "(root not here yet)"
			}
			c.printf("  %s → %s   in %s %s\n", b.ID, b.To, b.Thread, title)
		}
	}
	if len(unresolved) > 0 {
		ids := make([]string, len(unresolved))
		for i, u := range unresolved {
			ids[i] = u.String()
		}
		c.printf("Not here (yet): %s\n", strings.Join(ids, ", "))
	}
	return nil
}

func runList(c *ctx, _ []string) error {
	f, err := c.filter()
	if err != nil {
		return err
	}
	idx, err := c.index()
	if err != nil {
		return err
	}
	threads, err := idx.List(f, c.o.limit)
	if err != nil {
		return err
	}
	if c.o.json {
		out := []threadJSON{}
		for _, t := range threads {
			out = append(out, toThreadJSON(t))
		}
		return c.printJSON(out)
	}
	if len(threads) == 0 {
		c.printf("No threads.\n")
	}
	for _, t := range threads {
		c.printThreadLine(t)
	}
	return nil
}

func runSearch(c *ctx, args []string) error {
	f, err := c.filter()
	if err != nil {
		return err
	}
	q := index.Query{Terms: args, Filter: f, Limit: c.o.limit}
	switch {
	case c.o.inTitle && c.o.inBody:
		return usagef("--title and --body exclude each other")
	case c.o.inTitle:
		q.Field = index.FieldTitle
	case c.o.inBody:
		q.Field = index.FieldBody
	}
	var terms []string
	for _, t := range args {
		if strings.TrimSpace(t) != "" {
			terms = append(terms, t)
		}
	}
	if len(terms) == 0 {
		return usagef("search needs a non-empty term")
	}
	idx, err := c.index()
	if err != nil {
		return err
	}
	results, err := idx.Search(q)
	if err != nil {
		return err
	}
	if c.o.json {
		type matchJSON struct {
			ID      string `json:"id"`
			Field   string `json:"field"`
			Snippet string `json:"snippet"`
		}
		type resultJSON struct {
			threadJSON
			Matches []matchJSON `json:"matches"`
		}
		out := []resultJSON{}
		for _, r := range results {
			rj := resultJSON{threadJSON: toThreadJSON(r.Thread), Matches: []matchJSON{}}
			for _, m := range r.Matches {
				rj.Matches = append(rj.Matches, matchJSON{m.ID.String(), m.Field, m.Snippet})
			}
			out = append(out, rj)
		}
		return c.printJSON(out)
	}
	if len(results) == 0 {
		c.printf("No matches.\n")
	}
	for _, r := range results {
		c.printThreadLine(r.Thread)
		for _, m := range r.Matches {
			c.printf("%-10s %s (%s): %s\n", "", m.ID, m.Field, m.Snippet)
		}
	}
	return nil
}

func runTags(c *ctx, _ []string) error {
	idx, err := c.index()
	if err != nil {
		return err
	}
	tags, err := idx.Tags()
	if err != nil {
		return err
	}
	if c.o.json {
		type tagJSON struct {
			Tag     string `json:"tag"`
			Threads int    `json:"threads"`
		}
		out := []tagJSON{}
		for _, t := range tags {
			out = append(out, tagJSON{t.Tag, t.Threads})
		}
		return c.printJSON(out)
	}
	if len(tags) == 0 {
		c.printf("No tags.\n")
	}
	for _, t := range tags {
		c.printf("%-24s %s\n", t.Tag, plural(t.Threads, "thread", "threads"))
	}
	return nil
}

// parseSince parses a duration, also accepting days, such as 2d.
func parseSince(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil || n < 0 {
			return 0, usagef("invalid --since %q: use a duration such as 90m, 12h, or 2d", s)
		}
		return time.Duration(n * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, usagef("invalid --since %q: use a duration such as 90m, 12h, or 2d", s)
	}
	return d, nil
}

func runUnread(c *ctx, _ []string) error {
	var since time.Duration
	if c.o.since != "" {
		var err error
		if since, err = parseSince(c.o.since); err != nil {
			return err
		}
	}
	id, err := c.identity()
	if err != nil {
		return err
	}
	idx, err := c.index()
	if err != nil {
		return err
	}
	b, err := c.board()
	if err != nil {
		return err
	}
	items, err := idx.Items()
	if err != nil {
		return err
	}
	cur, existed, err := b.LoadCursor(id.harness, id.session)
	if err != nil {
		return err
	}
	if !existed {
		// A new reader has seen everything but the lookback.
		cutoff := c.env.Now().Add(-since).Unix()
		for _, it := range items {
			if since == 0 || it.CreatedUnix < cutoff {
				cur.Mark(it.ID.Node, it.ID.N)
			}
		}
	} else if c.o.since != "" {
		fmt.Fprintln(c.env.Stderr, "agora: warning: --since applies only to a reader's first unread; use agora list or search for earlier posts.")
	}
	var unseen []index.Item
	for _, it := range items {
		if it.Harness == id.harness && it.Session == id.session {
			cur.Mark(it.ID.Node, it.ID.N)
			continue
		}
		if !cur.Seen(it.ID.Node, it.ID.N) {
			unseen = append(unseen, it)
		}
	}
	var entries []index.Entry
	for _, it := range unseen {
		e, ok, err := idx.Get(it.ID)
		if err != nil {
			return err
		}
		if ok {
			entries = append(entries, e)
		}
	}
	if !c.o.peek {
		for _, it := range unseen {
			cur.Mark(it.ID.Node, it.ID.N)
		}
	}
	if !c.o.peek || !existed {
		if err := b.SaveCursor(id.harness, id.session, cur); err != nil {
			return err
		}
	}

	if c.o.json {
		out := struct {
			NewReader bool       `json:"new_reader"`
			Posts     []postJSON `json:"posts"`
		}{NewReader: !existed, Posts: []postJSON{}}
		for _, e := range entries {
			out.Posts = append(out.Posts, toPostJSON(e))
		}
		return c.printJSON(out)
	}
	if !existed {
		from := "now"
		if since > 0 {
			from = c.o.since + " ago"
		}
		fmt.Fprintf(c.env.Stderr, "agora: new reader: counting from %s; use agora list or search for earlier posts.\n", from)
	}
	if len(entries) == 0 {
		c.printf("Nothing unread.\n")
		return nil
	}
	titles := map[post.ID]string{}
	roots := []post.ID{}
	for _, e := range entries {
		if _, ok := titles[e.ThreadID()]; !ok {
			roots = append(roots, e.ThreadID())
			titles[e.ThreadID()] = ""
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Less(roots[j]) })
	for _, r := range roots {
		info, err := idx.ThreadInfo(r)
		if err != nil {
			return err
		}
		titles[r] = info.Title
	}
	for _, e := range entries {
		context := ""
		if !e.IsRoot() {
			context = "in " + e.Thread.String() + " " + titles[e.Thread]
		}
		if e.IsRoot() {
			c.printf("%s\n", e.Title)
		}
		c.printPost(e, false, context)
	}
	return nil
}
