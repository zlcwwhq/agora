// Package cli implements agora's command line, as api/cli.md specifies it.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/zlcwwhq/agora/internal/board"
	"github.com/zlcwwhq/agora/internal/cursor"
	"github.com/zlcwwhq/agora/internal/index"
	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/internal/web"
)

// Env is everything a command uses from its process.
type Env struct {
	// Args are the arguments after the program name.
	Args   []string
	Getenv func(string) string
	// Dir is the working directory, from which commands find the board and
	// in which init creates it. Empty means unknown.
	Dir string
	// GOOS is the operating system, which decides where home and state
	// directories are.
	GOOS           string
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Now            func() time.Time
	// Context stops commands that run until they are interrupted, serve. If
	// it is nil, they stop on an interrupt signal.
	Context context.Context
	// Version is agora's version, as the go command stamps it into the
	// binary: a release's tag or a pseudo-version. Empty means unknown.
	Version string
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
	addr      string
	set       map[string]bool
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

var commands = []command{
	{name: "init", args: "--node NAME [--resume]", summary: "record this machine's node name and create the board", run: runInit,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.StringVar(&o.node, "node", "", "this machine's node `name`")
			fs.BoolVar(&o.resume, "resume", false, "the node's existing directory, posts/NAME, is this machine's own")
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
	{name: "delete", args: "ID", summary: "delete a post of yours that this machine wrote", run: runDelete, positional: 1},
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
	{name: "serve", args: "[--addr ADDR] [--as human/NAME]", summary: "show the board in a browser, with forms for a person", run: runServe,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.StringVar(&o.addr, "addr", "127.0.0.1:7777", "listen on `ADDR`; port 0 picks a free port")
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
	if env.Args[0] == "--version" {
		if len(env.Args) > 1 {
			return usagef("--version takes no arguments")
		}
		version := env.Version
		if version == "" {
			version = "(devel)"
		}
		fmt.Fprintf(env.Stdout, "agora %s\n", version)
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
	fs.StringVar(&o.board, "board", "", "the board `directory`; default $AGORA_DIR, else the nearest .agora")
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
	fmt.Fprintln(w, "agora --version prints agora's version.")
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

// errNoBoard means that no board was found, which whoami reports rather than
// fail.
var errNoBoard = errors.New("no board found")

// board opens the board that commands other than init use, as api/cli.md's
// Finding the board specifies.
func (c *ctx) board() (*board.Board, error) {
	if c.b != nil {
		return c.b, nil
	}
	root := c.namedBoard()
	if root == "" {
		// Without a home directory, there is no ~/.agora to fall back on.
		home, _ := board.Home(c.env.Getenv, c.env.GOOS)
		found, err := board.Find(c.env.Dir, home)
		if err != nil {
			return nil, err
		}
		if found == "" {
			from := c.env.Dir
			if from == "" {
				from = "an unknown working directory"
			}
			return nil, fmt.Errorf("%w from %s: run agora init --node NAME in the project's directory, or pass --board",
				errNoBoard, from)
		}
		root = found
	}
	return c.open(root)
}

// namedBoard returns the board that --board or AGORA_DIR names, or "".
func (c *ctx) namedBoard() string {
	if c.o.board != "" {
		return c.o.board
	}
	return c.env.Getenv("AGORA_DIR")
}

// open opens the board at root and remembers it.
func (c *ctx) open(root string) (*board.Board, error) {
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
		return nil, fmt.Errorf("no board at %s: run %s", b.Root, b.InitHint())
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
		"(Claude Code, Codex, and Antigravity are detected through CLAUDE_CODE_SESSION_ID, CODEX_THREAD_ID, " +
		"and ANTIGRAVITY_CONVERSATION_ID)")
}

// detected are the harnesses that identify their sessions to the commands
// they run, in order of precedence.
var detected = []struct{ variable, harness string }{
	{"CLAUDE_CODE_SESSION_ID", "claude-code"},
	{"CODEX_THREAD_ID", "codex"},
	{"ANTIGRAVITY_CONVERSATION_ID", "antigravity"},
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
	// Like git init, init doesn't search: it creates the board where it's told,
	// or in the working directory.
	root := c.namedBoard()
	if root == "" {
		if c.env.Dir == "" {
			return errors.New("can't tell the working directory: pass --board")
		}
		root = filepath.Join(c.env.Dir, board.DirName)
	}
	b, err := c.open(root)
	if err != nil {
		return err
	}
	wt, err := board.LinkedWorktree(b.Root)
	if err != nil {
		return err
	}
	if wt.Root != "" {
		instead := "outside the repository's worktrees"
		if wt.Main != "" {
			instead = "in its main worktree, " + wt.Main
		}
		return fmt.Errorf("%s would be inside the linked worktree %s, whose commands use the main worktree's board, "+
			"and removing the worktree would remove it: run agora init %s", b.Root, wt.Root, instead)
	}
	if err := b.Init(c.o.node, c.o.resume); err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(map[string]string{"board": b.Root, "node": c.o.node, "state": b.State})
	}
	c.printf("This machine is node %s of %s.\nIts local state is in %s.\n", c.o.node, b.Root, b.State)
	c.printf("Synchronize %s with a Mutagen session of its own, ignoring /.tmp/,\n"+
		"and exclude it from any synchronization of the directory that contains it.\n", b.Root)
	return nil
}

func runWhoami(c *ctx, _ []string) error {
	id, err := c.identity()
	if err != nil {
		return err
	}
	var root, node string
	b, err := c.board()
	switch {
	case errors.Is(err, errNoBoard):
	case err != nil:
		return err
	default:
		root = b.Root
		node, err = b.Node()
		if err != nil && !errors.Is(err, board.ErrNotInitialized) {
			return err
		}
	}
	if c.o.json {
		return c.printJSON(map[string]string{
			"author": post.Author(id.harness, id.session, node), "harness": id.harness,
			"session": id.session, "node": node, "source": id.source, "board": root,
		})
	}
	switch {
	case root == "":
		root, node = "(none: run agora init --node NAME in the project's directory)", "(none)"
	case node == "":
		node = "(none: run " + b.InitHint() + ")"
	}
	c.printf("harness  %s\nsession  %s (%s)\nnode     %s\nboard    %s\n", id.harness, id.session, id.source, node, root)
	return nil
}

func (c *ctx) publish(p post.Post) error {
	id, err := c.identity()
	if err != nil {
		return err
	}
	b, p, err := c.publishAs(id, p)
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(map[string]string{"id": p.ID.String(), "thread": p.ThreadID().String(), "board": b.Root})
	}
	// The id stays alone on standard output, for callers that capture it.
	c.printf("%s\n", p.ID)
	fmt.Fprintf(c.env.Stderr, "agora: posted %s to %s\n", p.ID, b.Root)
	return nil
}

// publishAs publishes p as id on the board, and starts id's cursor if it is a
// new reader. The post is published once it returns no error.
func (c *ctx) publishAs(id identity, p post.Post) (*board.Board, post.Post, error) {
	b, err := c.board()
	if err != nil {
		return nil, p, err
	}
	p.Harness, p.Session = id.harness, id.session
	p.Created = c.env.Now().Format(time.RFC3339)
	p, err = b.Publish(p)
	if err != nil {
		return nil, p, err
	}
	// The post is published, so failing to start the reader's cursor only
	// warns: reporting failure would make callers retry and post twice.
	if err := c.startCursor(b, id); err != nil {
		fmt.Fprintf(c.env.Stderr, "agora: warning: published %s, but can't save this reader's unread cursor: %v\n", p.ID, err)
	}
	return b, p, nil
}

// The rules for what post and reply publish, which the web interface's forms
// share.
var (
	errNoTitle = usageError{"a thread needs a title"}
	errNoBody  = usageError{"a reply needs a body"}
)

// checkTitle returns a thread's title as post publishes it, or why post
// rejects it: errNoTitle or another usage error.
func checkTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	switch {
	case title == "":
		return "", errNoTitle
	case strings.ContainsAny(title, "\r\n"):
		return "", usagef("a title is a single line")
	case !utf8.ValidString(title):
		return "", usagef("the title isn't valid UTF-8")
	}
	return title, nil
}

// checkReplyBody returns errNoBody if a reply's body is blank.
func checkReplyBody(body string) error {
	if strings.TrimSpace(body) == "" {
		return errNoBody
	}
	return nil
}

// startCursor gives a reader without a cursor one that has seen every post in
// the index, so that posts arriving later, such as answers, are unread for it.
func (c *ctx) startCursor(b *board.Board, id identity) error {
	cur, existed, err := b.LoadCursor(id.harness, id.session)
	if err != nil || existed {
		return err
	}
	idx, err := c.index()
	if err != nil {
		return err
	}
	items, err := idx.Items()
	if err != nil {
		return err
	}
	c.markNew(cur, items, 0)
	return b.SaveCursor(id.harness, id.session, cur)
}

func runPost(c *ctx, _ []string) error {
	title, err := checkTitle(c.o.title)
	if errors.Is(err, errNoTitle) {
		return usagef("usage: agora post --title TITLE [-t TAG]... [-m BODY]")
	}
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
	if err := checkReplyBody(body); err != nil {
		return usagef("%v: pass -m BODY or -f FILE", err)
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
		return errUnknown(target)
	}
	if e.Deleted != "" {
		return errDeletedTarget(target)
	}
	return c.publish(post.Post{Thread: e.ThreadID(), ReplyTo: e.ID, Tags: tags, Body: body})
}

// errDeletedTarget is why reply refuses to reply to target, a deleted post.
func errDeletedTarget(target post.ID) error {
	return fmt.Errorf("can't reply to %s, which is deleted: reply to another post in its thread", target)
}

func runDelete(c *ctx, args []string) error {
	target, err := c.id(args[0])
	if err != nil {
		return err
	}
	id, err := c.identity()
	if err != nil {
		return err
	}
	b, p, already, err := c.deleteAs(id, target)
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(map[string]string{"id": p.ID.String(), "deleted": p.Deleted, "board": b.Root})
	}
	c.printf("%s\n", p.ID)
	if already {
		fmt.Fprintf(c.env.Stderr, "agora: %s was already deleted, %s\n", p.ID, when(p.Deleted))
	} else {
		fmt.Fprintf(c.env.Stderr, "agora: deleted %s from %s\n", p.ID, b.Root)
	}
	return nil
}

// deleteAs deletes target as id, as delete does, and returns the deleted
// post and whether it was deleted already.
func (c *ctx) deleteAs(id identity, target post.ID) (*board.Board, post.Post, bool, error) {
	idx, err := c.index()
	if err != nil {
		return nil, post.Post{}, false, err
	}
	if _, ok, err := idx.Get(target); err != nil || !ok {
		if err == nil {
			err = errUnknown(target)
		}
		return nil, post.Post{}, false, err
	}
	b, err := c.board()
	if err != nil {
		return nil, post.Post{}, false, err
	}
	p, already, err := b.Delete(target, id.harness, id.session, c.env.Now().Format(time.RFC3339))
	if err != nil {
		return nil, post.Post{}, false, err
	}
	// Bring the index up to date now, so that it drops the post's text
	// before the next command. Failing to only delays that.
	c.close()
	c.idx = nil
	if _, err := c.index(); err != nil {
		fmt.Fprintf(c.env.Stderr, "agora: warning: deleted %s, but can't update the index: %v\n", p.ID, err)
	}
	return b, p, already, nil
}

// errUnknown is the error for a post that isn't in the index.
type errUnknown post.ID

func (e errUnknown) Error() string {
	return fmt.Sprintf("unknown post %s: it may not have reached this machine yet", post.ID(e))
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
	Deleted string   `json:"deleted"`
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
		Title: e.Title, Tags: tags, Body: e.Body, Refs: refs, Deleted: e.Deleted,
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
	Deleted      string   `json:"deleted"`
}

func toThreadJSON(t index.Thread) threadJSON {
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	return threadJSON{
		ID: t.ID.String(), Title: t.Title, Author: post.Author(t.Harness, t.Session, t.ID.Node),
		Created: t.Created, LastActivity: t.LastActivity, Replies: t.Replies, Tags: tags, Deleted: t.Deleted,
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
	switch {
	case t.Deleted != "":
		title = post.DeletedTitle
	case title == "":
		title = "(root not here yet)"
	}
	c.printf("%-10s %s\n", t.ID, title)
	meta := []string{plural(t.Replies, "reply", "replies")}
	if a := post.Author(t.Harness, t.Session, t.ID.Node); a != "" {
		meta = append(meta, a)
	}
	if t.LastActivity != "" {
		meta = append(meta, "active "+when(t.LastActivity))
	}
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
	if e.Deleted != "" {
		parts = append(parts, "deleted "+when(e.Deleted))
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
		return errUnknown(target)
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
			switch {
			case b.RootDeleted:
				title = post.DeletedTitle
			case title == "":
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

// markNew starts a new reader's cursor: it has seen every post in items but
// those created within the lookback since.
func (c *ctx) markNew(cur *cursor.Cursor, items []index.Item, since time.Duration) {
	var cutoff int64
	if since > 0 {
		cutoff = c.env.Now().Add(-since).Unix()
	}
	for _, it := range items {
		if since == 0 || it.CreatedUnix < cutoff {
			cur.Mark(it.ID.Node, it.ID.N)
		}
	}
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
		c.markNew(cur, items, since)
	} else if c.o.since != "" {
		fmt.Fprintln(c.env.Stderr, "agora: warning: --since applies only to a new reader, before its first unread, post, or reply; use agora list or search for earlier posts.")
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
		// Deleted posts are left out, but marked seen below.
		if ok && e.Deleted == "" {
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

func runServe(c *ctx, _ []string) error {
	b, err := c.board()
	if err != nil {
		return err
	}
	// Fail now, as other commands do, if the board doesn't exist.
	if _, err := c.index(); err != nil {
		return err
	}
	c.close()
	c.idx = nil
	node, err := b.Node()
	if err != nil && !errors.Is(err, board.ErrNotInitialized) {
		return err
	}
	ln, err := net.Listen("tcp", c.o.addr)
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()
	author, publish, err := c.forms(b, node, ln.Addr())
	if err != nil {
		return err
	}
	var remove func(post.ID) error
	var mine func(post.Post) bool
	if publish != nil {
		remove, mine = c.personDelete(b, node)
	}
	url := "http://" + displayAddr(ln.Addr()) + "/"
	if c.o.json {
		if err := c.printJSON(map[string]string{"url": url, "board": b.Root, "author": author}); err != nil {
			return err
		}
	} else {
		c.printf("Serving %s at %s\n", b.Root, url)
		if author != "" {
			c.printf("Its forms post as %s.\n", author)
		}
		c.printf("Press Ctrl-C to stop.\n")
	}

	stop := c.env.Context
	if stop == nil {
		var cancel context.CancelFunc
		stop, cancel = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
	}
	return web.Serve(stop, ln, web.Config{
		// Every page brings its own index up to date, so that posts that
		// arrive while serving appear.
		Open: func() (*index.DB, error) {
			page := &ctx{env: c.env, o: c.o, b: b}
			return page.index()
		},
		Board: b.Root, Node: node, Now: c.env.Now,
		Logf: func(format string, args ...any) {
			fmt.Fprintf(c.env.Stderr, "agora: "+format+"\n", args...)
		},
		Post: publish, Author: author, Delete: remove, Mine: mine,
	})
}

// person is the harness of people, whom serve's forms post as.
const person = "human"

// forms returns whom serve's forms post as, and the function that posts for
// them, or "" and nil if the pages only read, as api/cli.md specifies: the
// identity must be a person's from --as or AGORA_AUTHOR, never detection,
// which names the agent session that started serve.
func (c *ctx) forms(b *board.Board, node string, addr net.Addr) (string, func(web.Draft) (post.ID, error), error) {
	if c.o.as == "" && c.env.Getenv("AGORA_AUTHOR") == "" {
		return "", nil, nil
	}
	id, err := c.identity()
	if err != nil {
		return "", nil, err
	}
	if id.harness != person {
		return "", nil, nil
	}
	who := id.harness + "/" + id.session
	switch {
	case node == "":
		fmt.Fprintf(c.env.Stderr, "agora: warning: pages only read, since %s can't post without a node name for the board: run %s\n",
			who, b.InitHint())
		return "", nil, nil
	case !web.IsLoopback(addr):
		fmt.Fprintf(c.env.Stderr, "agora: warning: pages only read, since forms are offered only on a loopback address, "+
			"such as the default, 127.0.0.1:7777, and not on %s\n", addr)
		return "", nil, nil
	}
	return post.Author(id.harness, id.session, node), func(d web.Draft) (post.ID, error) { return c.formPost(b, id, d) }, nil
}

// personDelete returns the functions with which the forms that forms offers
// delete the person's posts: one that deletes as delete does, and one that
// says which posts it would delete, the person's that this machine wrote.
func (c *ctx) personDelete(b *board.Board, node string) (func(post.ID) error, func(post.Post) bool) {
	// forms has checked the identity.
	id, _ := c.identity()
	remove := func(target post.ID) error { return c.formDelete(b, id, target) }
	mine := func(p post.Post) bool {
		return p.ID.Node == node && p.Harness == id.harness && p.Session == id.session
	}
	return remove, mine
}

// formDelete deletes target as id, as delete does.
func (c *ctx) formDelete(b *board.Board, id identity, target post.ID) error {
	dc := &ctx{env: c.env, o: c.o, b: b}
	defer dc.close()
	_, _, _, err := dc.deleteAs(id, target)
	var unknown errUnknown
	switch {
	case errors.As(err, &unknown):
		return web.Missing("There is no post " + target.String() + " here. It may not have reached this machine yet.")
	case errors.Is(err, board.ErrNotYours):
		return web.Forbidden(sentence(err.Error()))
	}
	return err
}

// formPost publishes what a form sent as id, with the rules of post and reply.
func (c *ctx) formPost(b *board.Board, id identity, d web.Draft) (post.ID, error) {
	pc := &ctx{env: c.env, o: c.o, b: b}
	defer pc.close()
	refuse := func(err error) (post.ID, error) {
		return post.ID{}, web.Refused(sentence(err.Error()))
	}
	tags, err := post.NormalizeTags(d.Tags)
	if err != nil {
		return refuse(err)
	}
	if !utf8.ValidString(d.Body) {
		return refuse(errors.New("the body isn't valid UTF-8"))
	}
	p := post.Post{Tags: tags, Body: post.NormalizeBody(d.Body)}
	if d.ReplyTo.IsZero() {
		if p.Title, err = checkTitle(d.Title); err != nil {
			return refuse(err)
		}
	} else {
		if err := checkReplyBody(p.Body); err != nil {
			return refuse(err)
		}
		idx, err := pc.index()
		if err != nil {
			return post.ID{}, err
		}
		e, ok, err := idx.Get(d.ReplyTo)
		if err != nil {
			return post.ID{}, err
		}
		if !ok {
			return post.ID{}, web.Missing("There is no post " + d.ReplyTo.String() + " here. It may not have reached this machine yet.")
		}
		if e.Deleted != "" {
			return refuse(errDeletedTarget(e.ID))
		}
		p.Thread, p.ReplyTo = e.ThreadID(), e.ID
	}
	_, p, err = pc.publishAs(id, p)
	return p.ID, err
}

// sentence makes a message start with a capital and end with a period, as
// pages show them.
func sentence(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	s = strings.ToUpper(string(r)) + s[n:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// displayAddr is the address to open in a browser: a server listening on
// every address is reached at the loopback address.
func displayAddr(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok || !tcp.IP.IsUnspecified() {
		return addr.String()
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(tcp.Port))
}
