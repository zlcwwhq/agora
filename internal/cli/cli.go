// Package cli is agora's command line, as api/cli.md specifies: every command
// but init, whoami, and serve is a client of a server, most of them on one of
// its boards, and serve runs that server.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zlcwwhq/agora/internal/client"
	"github.com/zlcwwhq/agora/internal/config"
	"github.com/zlcwwhq/agora/internal/post"
)

// Env is everything a command uses from its process.
type Env struct {
	// Args are the arguments after the program name.
	Args   []string
	Getenv func(string) string
	// Dir is the working directory, from which commands find the
	// configuration and in which init writes it. Empty means unknown.
	Dir string
	// GOOS is the operating system, which decides where home and state
	// directories are.
	GOOS string
	// Hostname returns the machine's host name, the default node.
	Hostname       func() (string, error)
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Now            func() time.Time
	// Context stops commands that run until they are interrupted, serve. If
	// it is nil, they stop on an interrupt signal.
	Context context.Context
	// Version is agora's version, as the go command stamps it into the
	// binary. Empty means unknown.
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
	// positional is the number of arguments, -1 for one or more, or -2 for
	// any number, which the command checks.
	positional int
}

type opts struct {
	config, server, as string
	model              string
	board              string
	json               bool
	node, author       string
	force              bool
	title              string
	tags               stringList
	noTags             bool
	message            string
	file               string
	limit              int
	inTitle, inBody    bool
	peek               bool
	since              string
	addTags, rmTags    stringList
	toMe, undo         bool
	thread             string
	addr, db           string
	set                map[string]bool
	threadOpts
	nameOpts
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func bodyFlags(fs *flag.FlagSet, o *opts) {
	fs.StringVar(&o.message, "m", "", "the `body`")
	fs.StringVar(&o.file, "f", "", "read the body from `FILE`; - is standard input")
}

var commands = []command{
	{name: "init", args: "[--board ID] [--node NAME] [--server HOST:PORT] [--author HARNESS/SESSION] [--force]", summary: "write the configuration file, .agora", run: runInit,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.StringVar(&o.node, "node", "", "this machine's node `name`; default the host name")
			fs.StringVar(&o.author, "author", "", "who you are when no harness is detected, `HARNESS/SESSION`")
			fs.BoolVar(&o.force, "force", false, "replace an existing file")
		}},
	{name: "whoami", summary: "show the identity, configuration, server, and board that commands use", run: runWhoami},
	{name: "boards", args: "[create ID [--title TITLE]]", summary: "list the server's boards, or create one", run: runBoards, positional: -2,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.StringVar(&o.title, "title", "", "the new board's `title`; default its id")
		}},
	{name: "post", args: "--title TITLE [-t TAG]... [-m BODY | -f FILE] [-a FILE]...", summary: "start a thread", run: runPost,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.StringVar(&o.title, "title", "", "the thread's `title`")
			fs.Var(&o.tags, "t", "a `tag`, repeatable")
			bodyFlags(fs, o)
			attachFlags(fs, o)
		}},
	{name: "reply", args: "ID [-t TAG]... [-m BODY | -f FILE] [-a FILE]...", summary: "reply to a post", run: runReply, positional: 1,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.Var(&o.tags, "t", "a `tag`, repeatable")
			bodyFlags(fs, o)
			attachFlags(fs, o)
		}},
	{name: "edit", args: "ID [--title TITLE] [-t TAG]... [--no-tags] [--add-tag TAG]... [--remove-tag TAG]... [-m BODY | -f FILE] [-a FILE]... [--detach NAME]...", summary: "change a post in place", run: runEdit, positional: 1,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.StringVar(&o.title, "title", "", "the thread's new `title`")
			fs.Var(&o.tags, "t", "a `tag` that replaces all of the post's tags, repeatable")
			fs.BoolVar(&o.noTags, "no-tags", false, "remove the post's tags")
			singleTagFlags(fs, o)
			bodyFlags(fs, o)
			attachFlags(fs, o)
			fs.Var(&o.detach, "detach", "remove the post's attachment `NAME`, repeatable")
		}},
	{name: "history", args: "ID", summary: "show every version of a post", run: runHistory, positional: 1},
	{name: "react", args: "ID REACTION [--undo]", summary: "react to a post, such as with ack, or take the reaction back", run: runReact, positional: 2,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.BoolVar(&o.undo, "undo", false, "take the reaction back")
		}},
	{name: "delete", args: "ID", summary: "delete a post for good, and a thread's first post with its thread", run: runDelete, positional: 1},
	threadCommand,
	moveCommand,
	{name: "show", args: "ID", summary: "show the thread that contains a post", run: runShow, positional: 1},
	downloadCommand,
	{name: "list", args: "[-t TAG]... [--author AUTHOR] [--state STATE] [-n N] [--skip K]", summary: "list threads, pinned ones first, by recent activity", run: runList,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.Var(&o.tags, "t", "require a `tag`, repeatable")
			fs.StringVar(&o.author, "author", "", "`HARNESS` or HARNESS/SESSION-PREFIX that started the thread")
			fs.IntVar(&o.limit, "n", post.DefaultLimit, "at most `N` threads")
			pageFlags(fs, o)
		}},
	{name: "search", args: "TERM... [--title | --body] [-t TAG]... [--author AUTHOR] [--state STATE] [-n N] [--skip K]", summary: "find threads by title and body", run: runSearch, positional: -1,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.BoolVar(&o.inTitle, "title", false, "match titles only")
			fs.BoolVar(&o.inBody, "body", false, "match bodies only")
			fs.Var(&o.tags, "t", "require a `tag` of the thread, repeatable")
			fs.StringVar(&o.author, "author", "", "`HARNESS` or HARNESS/SESSION-PREFIX of the matching post")
			fs.IntVar(&o.limit, "n", post.DefaultLimit, "at most `N` threads")
			pageFlags(fs, o)
		}},
	{name: "tags", summary: "list tags by use", run: runTags},
	iconsCommand,
	namesCommand,
	claimCommand,
	{name: "unread", args: "[--to-me] [--thread ID] [--peek] [--since DURATION]", summary: "show posts this reader hasn't seen, and those edited since it saw them", run: runUnread,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.BoolVar(&o.toMe, "to-me", false, "only the posts of threads where this reader posted, and those that mention it")
			fs.StringVar(&o.thread, "thread", "", "only the posts of the thread that contains post `ID`")
			fs.BoolVar(&o.peek, "peek", false, "don't mark them seen")
			fs.StringVar(&o.since, "since", "", "for a new reader, also posts created within `DURATION`, such as 90m or 2d")
		}},
	backupCommand,
	{name: "serve", args: "[--addr HOST:PORT] [--db FILE] [--as human/NAME]", summary: "run the server of the boards: the API and the web pages", run: runServe,
		flags: func(fs *flag.FlagSet, o *opts) {
			fs.StringVar(&o.addr, "addr", "", "listen on `HOST:PORT`; default the configured server; port 0 picks a free port")
			fs.StringVar(&o.db, "db", "", "the board's database `FILE`; default board.db in agora's state directory")
		}},
}

// Run runs a command and returns its exit status.
func Run(env Env) int {
	err := run(env)
	if err == nil || errors.Is(err, flag.ErrHelp) {
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
	fs.StringVar(&o.config, "config", "", "the configuration `FILE`; default $AGORA_CONFIG, else the nearest .agora")
	fs.StringVar(&o.server, "server", "", "the server, `HOST:PORT`, overriding the configuration")
	fs.StringVar(&o.board, "board", "", "the board's `ID`, overriding the configuration")
	fs.BoolVar(&o.json, "json", false, "print JSON")
	fs.StringVar(&o.as, "as", "", "act as `HARNESS/SESSION`")
	fs.StringVar(&o.model, "model", "", "the `MODEL` that writes, such as claude-opus-5-5; default $AGORA_MODEL")
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
	c := &ctx{env: env, o: o, stdout: env.Stdout}
	if o.json {
		// Standard output stays empty on failure.
		c.stdout = &c.buf
	}
	err = cmd.run(c, args)
	c.warnDisplaced()
	if err != nil {
		return err
	}
	if o.json {
		_, err := io.Copy(env.Stdout, &c.buf)
		return err
	}
	return nil
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
	fmt.Fprintln(w, "agora: a message board for agents and the people who work with them")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "usage: agora COMMAND [ARGS] [--config FILE] [--server HOST:PORT] [--board ID] [--json] [--as HARNESS/SESSION] [--model MODEL]")
	fmt.Fprintln(w)
	for _, c := range commands {
		fmt.Fprintf(w, "  %-8s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run agora COMMAND -h for its flags. Ids are numbers: agora show 12.")
	fmt.Fprintln(w, "agora --version prints agora's version.")
}

// ctx is a running command's state.
type ctx struct {
	env    Env
	o      *opts
	stdout io.Writer
	buf    bytes.Buffer
	cfg    *config.Config
	// clients are the clients that the command made, whose answers may say
	// that another session took this one's name.
	clients []*client.Client
}

func (c *ctx) home() string {
	if c.env.GOOS == "windows" {
		return c.env.Getenv("USERPROFILE")
	}
	return c.env.Getenv("HOME")
}

// config returns the configuration, loading it once.
func (c *ctx) config() (config.Config, error) {
	if c.cfg == nil {
		cfg, err := config.Load(c.o.config, c.o.server, c.o.board, config.Env{
			Dir: c.env.Dir, Home: c.home(), Getenv: c.env.Getenv, Hostname: c.env.Hostname,
		})
		if err != nil {
			return cfg, err
		}
		c.cfg = &cfg
	}
	return *c.cfg, nil
}

var detected = []struct{ variable, harness string }{
	{"CLAUDE_CODE_SESSION_ID", "claude-code"},
	{"CODEX_THREAD_ID", "codex"},
	{"ANTIGRAVITY_CONVERSATION_ID", "antigravity"},
}

// identity resolves the identity, as api/cli.md's Identity says, and its
// source. With detect false, detection is skipped, as serve does. It returns
// a zero identity, without an error, if there is none.
func (c *ctx) identity(detect bool) (post.Identity, string, error) {
	cfg, err := c.config()
	if err != nil {
		return post.Identity{}, "", err
	}
	parse := func(s, label, source string) (post.Identity, string, error) {
		harness, session, err := post.ParseAuthor(s)
		if err != nil {
			return post.Identity{}, "", fmt.Errorf("%s: %w", label, err)
		}
		return post.Identity{Harness: harness, Session: session, Node: cfg.Node}, source, nil
	}
	if c.o.as != "" {
		return parse(c.o.as, "--as", "flag")
	}
	if a := c.env.Getenv("AGORA_AUTHOR"); a != "" {
		return parse(a, "AGORA_AUTHOR", "env")
	}
	if detect {
		for _, d := range detected {
			if session := c.env.Getenv(d.variable); session != "" {
				if !post.ValidSession(session) {
					return post.Identity{}, "", fmt.Errorf("%s is %q, which isn't a valid session id", d.variable, session)
				}
				return post.Identity{Harness: d.harness, Session: session, Node: cfg.Node}, "detected", nil
			}
		}
	}
	if cfg.Author != "" {
		return parse(cfg.Author, "[whoami] author in "+cfg.Path, "config")
	}
	return post.Identity{}, "", nil
}

// author returns the identity that a command needs.
func (c *ctx) author() (post.Identity, error) {
	who, _, err := c.identity(true)
	if err == nil && who.IsZero() {
		err = errors.New("no identity: pass --as HARNESS/SESSION, set AGORA_AUTHOR, or set author in [whoami] of .agora; " +
			"people use the harness human, such as --as human/alice")
	}
	return who, err
}

// errNoBoard is the failure of a command that needs a board without one.
var errNoBoard = errors.New("no board: pass --board ID, set AGORA_BOARD, or set id in [board] of .agora; agora boards lists the server's boards")

// client returns a client of the configured server and board, acting as
// the identity if need is true.
func (c *ctx) client(need bool) (*client.Client, error) {
	cfg, err := c.config()
	if err != nil {
		return nil, err
	}
	if cfg.Board == "" {
		return nil, errNoBoard
	}
	return c.serverClient(need)
}

// serverClient returns a client of the configured server, for calls that
// need no board.
func (c *ctx) serverClient(need bool) (*client.Client, error) {
	cfg, err := c.config()
	if err != nil {
		return nil, err
	}
	var who post.Identity
	var source string
	if need {
		if who, err = c.author(); err != nil {
			return nil, err
		}
		_, source, _ = c.identity(true)
	} else if who, source, err = c.identity(true); err != nil {
		// Commands that only read need no identity, but send one when
		// they have it, which renews the last activity of a carrier's
		// name.
		who = post.Identity{}
	}
	model, err := c.model()
	if err != nil {
		return nil, err
	}
	cl := client.New(cfg.URL(), cfg.Board, who)
	cl.Model = model
	if harness, session := c.detected(); session != "" && (source == "flag" || source == "env") && (harness != who.Harness || session != who.Session) {
		cl.Carrier = harness + "/" + session
	}
	c.clients = append(c.clients, cl)
	return cl, nil
}

// model returns the model that writes: --model, else AGORA_MODEL.
func (c *ctx) model() (string, error) {
	if c.o.model != "" {
		if err := post.CheckModel(c.o.model); err != nil {
			return "", usageError{"--model: " + err.Error()}
		}
		return c.o.model, nil
	}
	m := c.env.Getenv("AGORA_MODEL")
	if err := post.CheckModel(m); err != nil {
		return "", fmt.Errorf("AGORA_MODEL: %w", err)
	}
	return m, nil
}

// detected returns the session that harness detection finds, if any.
func (c *ctx) detected() (harness, session string) {
	for _, d := range detected {
		if s := c.env.Getenv(d.variable); s != "" && post.ValidSession(s) {
			return d.harness, s
		}
	}
	return "", ""
}

// warnDisplaced warns once if an answer said that another session took
// this session's name.
func (c *ctx) warnDisplaced() {
	for _, cl := range c.clients {
		if cl.Displaced != "" {
			c.warnf("this session no longer carries %s: another session took it; agora claim says which, and agora claim %s --take takes it back",
				cl.Displaced, cl.Displaced[strings.Index(cl.Displaced, "/")+1:])
			return
		}
	}
}

func (c *ctx) ctx() context.Context {
	if c.env.Context != nil {
		return c.env.Context
	}
	return context.Background()
}

func (c *ctx) id(s string) (int64, error) {
	id, err := post.ParseID(s)
	if err != nil {
		return 0, usageError{err.Error()}
	}
	return id, nil
}

func (c *ctx) tags() ([]string, error) {
	tags, err := post.Tags(c.o.tags)
	if err != nil {
		return nil, usageError{err.Error()}
	}
	return tags, nil
}

// hasBody reports whether -m or -f was given.
func (c *ctx) hasBody() bool { return c.o.set["m"] || c.o.set["f"] }

// body returns the body from -m or -f, or the empty string. Standard input is
// read only for -f -, so that a command never waits on an input that nobody
// closes.
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
	return body, nil
}

func (c *ctx) filter() (post.Filter, error) {
	tags, err := c.tags()
	if err != nil {
		return post.Filter{}, err
	}
	if c.o.author != "" {
		if _, _, err := post.AuthorFilter(c.o.author); err != nil {
			return post.Filter{}, usageError{err.Error()}
		}
	}
	if c.o.limit < 1 {
		return post.Filter{}, usagef("-n must be at least 1")
	}
	if err := c.checkPage(); err != nil {
		return post.Filter{}, err
	}
	return post.Filter{Tags: tags, Author: c.o.author, Limit: c.o.limit, State: c.o.state, Skip: c.o.skip}, nil
}

func (c *ctx) printJSON(v any) error {
	e := json.NewEncoder(c.stdout)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	return e.Encode(v)
}

func (c *ctx) printf(format string, args ...any) { fmt.Fprintf(c.stdout, format, args...) }

func (c *ctx) warnf(format string, args ...any) {
	fmt.Fprintf(c.env.Stderr, "agora: warning: "+format+"\n", args...)
}

// postURL returns a post's address in the server's pages.
func (c *ctx) postURL(thread, id int64) string {
	cfg, _ := c.config()
	return fmt.Sprintf("%s/b/%s/t/%d#%d", cfg.URL(), cfg.Board, thread, id)
}

func runInit(c *ctx, _ []string) error {
	path := c.o.config
	if path == "" {
		path = c.env.Getenv("AGORA_CONFIG")
	}
	if path == "" {
		if c.env.Dir == "" {
			return errors.New("can't tell the working directory: pass --config FILE")
		}
		path = filepath.Join(c.env.Dir, config.FileName)
	}
	host, port := config.DefaultHost, config.DefaultPort
	if c.o.server != "" {
		var err error
		if host, port, err = config.ParseServer(c.o.server); err != nil {
			return usageError{err.Error()}
		}
	}
	if c.o.author != "" {
		if _, _, err := post.ParseAuthor(c.o.author); err != nil {
			return usageError{"--author: " + err.Error()}
		}
	}
	if c.o.node != "" && !post.ValidNode(c.o.node) {
		return usagef("invalid --node %q: want 1 to 32 lowercase letters, digits and '-', starting with a letter", c.o.node)
	}
	if c.o.board != "" && !post.ValidBoard(c.o.board) {
		return usagef("invalid --board %q: want 1 to 32 lowercase letters, digits and '-', starting with a letter", c.o.board)
	}
	if err := config.Write(path, host, port, c.o.board, c.o.node, c.o.author, c.o.force); err != nil {
		return err
	}
	// What commands that use the file get.
	c.o.config, c.o.server, c.o.board = path, "", ""
	cfg, err := c.config()
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(map[string]string{"config": path, "server": cfg.URL(), "board": cfg.Board, "node": cfg.Node})
	}
	board := cfg.Board
	if board == "" {
		board = "(none: commands need --board or AGORA_BOARD)"
	}
	c.printf("wrote %s\nserver %s\nboard  %s\nnode   %s\n", path, cfg.URL(), board, cfg.Node)
	return nil
}

func runWhoami(c *ctx, _ []string) error {
	cfg, err := c.config()
	if err != nil {
		return err
	}
	who, source, err := c.identity(true)
	if err != nil {
		return err
	}
	model, err := c.model()
	if err != nil {
		return err
	}
	out := struct {
		Author  string `json:"author"`
		Harness string `json:"harness"`
		Session string `json:"session"`
		Node    string `json:"node"`
		Model   string `json:"model"`
		Source  string `json:"source"`
		Config  string `json:"config"`
		Server  string `json:"server"`
		Board   string `json:"board"`
	}{"", who.Harness, who.Session, cfg.Node, model, source, cfg.Path, cfg.URL(), cfg.Board}
	if !who.IsZero() {
		out.Author = who.Author()
	}
	if c.o.json {
		return c.printJSON(out)
	}
	if who.IsZero() {
		c.printf("author   (none: pass --as, set AGORA_AUTHOR, or set author in [whoami] of .agora)\n")
	} else {
		c.printf("author   %s\nharness  %s\nsession  %s (%s)\n", out.Author, who.Harness, who.Session, source)
	}
	cfgPath := cfg.Path
	if cfgPath == "" {
		cfgPath = "(none: defaults; agora init writes one)"
	}
	board := cfg.Board
	if board == "" {
		board = "(none: pass --board ID, set AGORA_BOARD, or set id in [board] of .agora)"
	}
	c.printf("node     %s\n", cfg.Node)
	if model != "" {
		c.printf("model    %s\n", model)
	}
	c.printf("config   %s\nserver   %s\nboard    %s\n", cfgPath, cfg.URL(), board)
	return nil
}

func runBoards(c *ctx, args []string) error {
	switch {
	case len(args) == 0:
		if c.o.set["title"] {
			return usagef("--title is for agora boards create")
		}
		cl, err := c.serverClient(false)
		if err != nil {
			return err
		}
		bs, err := cl.Boards(c.ctx())
		if err != nil {
			return err
		}
		if c.o.json {
			return c.printJSON(bs)
		}
		if len(bs) == 0 {
			c.printf("no boards: agora boards create ID creates one\n")
		}
		for _, b := range bs {
			c.printf("%-16s %s\n", b.ID, b.Title)
			meta := []string{plural(b.Threads, "thread", "threads"), plural(b.Posts, "post", "posts")}
			if b.LastActivity != "" {
				meta = append(meta, "active "+when(b.LastActivity))
			}
			c.printf("%-16s %s\n", "", strings.Join(meta, " · "))
		}
		return nil
	case len(args) == 2 && args[0] == "create":
		if !post.ValidBoard(args[1]) {
			return usagef("invalid board %q: want 1 to 32 lowercase letters, digits and '-', starting with a letter", args[1])
		}
		cl, err := c.serverClient(true)
		if err != nil {
			return err
		}
		b, err := cl.CreateBoard(c.ctx(), post.NewBoard{ID: args[1], Title: c.o.title})
		if err != nil {
			return err
		}
		cfg, _ := c.config()
		url := cfg.URL() + "/b/" + b.ID + "/"
		if c.o.json {
			return c.printJSON(map[string]string{"id": b.ID, "title": b.Title, "url": url})
		}
		c.printf("created board %s  %s\n", b.ID, url)
		return nil
	}
	return usagef("usage: agora boards [create ID [--title TITLE]]")
}

func (c *ctx) printPublished(verb string, pub post.Published) error {
	url := c.postURL(pub.Thread, pub.ID)
	if c.o.json {
		return c.printJSON(struct {
			ID     int64  `json:"id"`
			Thread int64  `json:"thread"`
			URL    string `json:"url"`
		}{pub.ID, pub.Thread, url})
	}
	c.printf("%s #%d  %s\n", verb, pub.ID, url)
	return nil
}

func runPost(c *ctx, _ []string) error {
	if !c.o.set["title"] {
		return usagef("post needs --title")
	}
	tags, err := c.tags()
	if err != nil {
		return err
	}
	body, err := c.body()
	if err != nil {
		return err
	}
	files, err := c.uploads()
	if err != nil {
		return err
	}
	cl, err := c.client(true)
	if err != nil {
		return err
	}
	pub, err := cl.Post(c.ctx(), post.NewThread{Title: c.o.title, Tags: tags, Body: body})
	if err != nil {
		return err
	}
	if err := c.attach(cl, "posted", pub.ID, files); err != nil {
		return err
	}
	return c.printPublished("posted", pub)
}

func runReply(c *ctx, args []string) error {
	id, err := c.id(args[0])
	if err != nil {
		return err
	}
	tags, err := c.tags()
	if err != nil {
		return err
	}
	if !c.hasBody() {
		return usagef("reply needs a body: -m BODY or -f FILE")
	}
	body, err := c.body()
	if err != nil {
		return err
	}
	files, err := c.uploads()
	if err != nil {
		return err
	}
	cl, err := c.client(true)
	if err != nil {
		return err
	}
	pub, err := cl.Reply(c.ctx(), id, post.NewReply{Tags: tags, Body: body})
	if err != nil {
		return err
	}
	if err := c.attach(cl, "replied", pub.ID, files); err != nil {
		return err
	}
	return c.printPublished("replied", pub)
}

func runEdit(c *ctx, args []string) error {
	id, err := c.id(args[0])
	if err != nil {
		return err
	}
	var e post.Edit
	if c.o.set["title"] {
		e.Title = &c.o.title
	}
	switch {
	case c.o.noTags && len(c.o.tags) > 0:
		return usagef("-t and --no-tags exclude each other")
	case (c.o.noTags || len(c.o.tags) > 0) && (len(c.o.addTags) > 0 || len(c.o.rmTags) > 0):
		return usagef("--add-tag and --remove-tag change single tags, so they exclude -t and --no-tags")
	case len(c.o.addTags) > 0 || len(c.o.rmTags) > 0:
		if err := c.singleTags(&e); err != nil {
			return err
		}
	case c.o.noTags:
		e.Tags = &[]string{}
	case len(c.o.tags) > 0:
		tags, err := c.tags()
		if err != nil {
			return err
		}
		e.Tags = &tags
	}
	if c.hasBody() {
		body, err := c.body()
		if err != nil {
			return err
		}
		e.Body = &body
	}
	files, err := c.uploads()
	if err != nil {
		return err
	}
	if e.Empty() && len(files) == 0 && len(c.o.detach) == 0 {
		return usagef("edit needs something to change: --title, -t, --no-tags, --add-tag, --remove-tag, -m, -f, -a, or --detach")
	}
	cl, err := c.client(true)
	if err != nil {
		return err
	}
	p, err := c.edit(cl, id, e)
	if err != nil {
		return err
	}
	if err := c.changeAttachments(cl, p.ID, !e.Empty(), files); err != nil {
		return err
	}
	url := c.postURL(p.Thread, p.ID)
	if c.o.json {
		return c.printJSON(struct {
			ID     int64  `json:"id"`
			Thread int64  `json:"thread"`
			Edited string `json:"edited"`
			URL    string `json:"url"`
		}{p.ID, p.Thread, p.Edited, url})
	}
	c.printf("edited #%d  %s\n", p.ID, url)
	return nil
}

func runDelete(c *ctx, args []string) error {
	id, err := c.id(args[0])
	if err != nil {
		return err
	}
	cl, err := c.client(true)
	if err != nil {
		return err
	}
	r, err := cl.Delete(c.ctx(), id)
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(r)
	}
	if r.ID == r.Thread {
		c.printf("deleted thread #%d with %s\n", r.ID, plural(len(r.Removed)-1, "reply", "replies"))
	} else {
		c.printf("deleted #%d from thread #%d\n", r.ID, r.Thread)
	}
	return nil
}

func when(t string) string {
	parsed, err := time.Parse(time.RFC3339, t)
	if err != nil {
		return t
	}
	return parsed.Format("2006-01-02 15:04 -07:00")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func hashTags(tags []string) string { return "#" + strings.Join(tags, " #") }

func (c *ctx) printThreadLine(t post.Thread) {
	c.printf("#%-6d %s\n", t.ID, t.Title)
	meta := append(threadFlags(t), plural(t.Replies, "reply", "replies"), t.Author, "active "+when(t.LastActivity))
	if len(t.Tags) > 0 {
		meta = append(meta, hashTags(t.Tags))
	}
	c.printf("%-7s %s\n", "", strings.Join(meta, " · "))
}

func (c *ctx) printPost(p post.Post, focus bool, context string) {
	marker := "──"
	if focus {
		marker = "▶▶"
	}
	parts := append(append([]string{"#" + strconv.FormatInt(p.ID, 10), p.Author}, via(p.Carrier, p.Model)...), when(p.Created))
	if p.ReplyTo != nil && *p.ReplyTo != p.Thread {
		parts = append(parts, fmt.Sprintf("reply to #%d", *p.ReplyTo))
	}
	if len(p.Tags) > 0 {
		parts = append(parts, hashTags(p.Tags))
	}
	if p.Edited != "" {
		edited := "edited " + when(p.Edited)
		if p.Editor != "" {
			edited += " by " + p.Editor
		}
		parts = append(parts, edited)
	}
	if context != "" {
		parts = append(parts, context)
	}
	c.printf("%s %s\n", marker, strings.Join(parts, " · "))
	if p.Title != "" && context != "" {
		c.printf("%s\n", p.Title)
	}
	if p.Body != "" {
		c.printf("%s", p.Body)
	}
	c.printReactions(p.Reactions)
	c.printAttachments(p)
	c.printf("\n")
}

func runShow(c *ctx, args []string) error {
	id, err := c.id(args[0])
	if err != nil {
		return err
	}
	cl, err := c.client(false)
	if err != nil {
		return err
	}
	s, err := cl.Show(c.ctx(), id)
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(s)
	}
	c.printf("#%d %s\n", s.Thread.ID, s.Thread.Title)
	meta := append(threadFlags(s.Thread), s.Thread.Author, plural(s.Thread.Replies, "reply", "replies"))
	if len(s.Thread.Tags) > 0 {
		meta = append(meta, hashTags(s.Thread.Tags))
	}
	c.printf("%s\n\n", strings.Join(meta, " · "))
	for _, p := range s.Posts {
		c.printPost(p, p.ID == s.Focus && s.Focus != s.Thread.ID, "")
	}
	if len(s.Backlinks) > 0 {
		c.printf("Referenced from other threads:\n")
		for _, b := range s.Backlinks {
			c.printf("  #%d in #%d %q → #%d\n", b.ID, b.Thread, b.Title, b.To)
		}
	}
	return nil
}

func runList(c *ctx, _ []string) error {
	f, err := c.filter()
	if err != nil {
		return err
	}
	cl, err := c.client(false)
	if err != nil {
		return err
	}
	ts, err := cl.List(c.ctx(), f)
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(ts)
	}
	if len(ts) == 0 {
		c.printf("no threads\n")
	}
	for _, t := range ts {
		c.printThreadLine(t)
	}
	return nil
}

func runSearch(c *ctx, args []string) error {
	if c.o.inTitle && c.o.inBody {
		return usagef("--title and --body exclude each other")
	}
	f, err := c.filter()
	if err != nil {
		return err
	}
	q := post.Query{Terms: args, Filter: f}
	switch {
	case c.o.inTitle:
		q.Field = post.FieldTitle
	case c.o.inBody:
		q.Field = post.FieldBody
	}
	cl, err := c.client(false)
	if err != nil {
		return err
	}
	rs, err := cl.Search(c.ctx(), q)
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(rs)
	}
	if len(rs) == 0 {
		c.printf("no matches\n")
	}
	for _, r := range rs {
		c.printThreadLine(r.Thread)
		for _, m := range r.Matches {
			c.printf("%-7s #%d %s: %s\n", "", m.ID, m.Field, m.Snippet)
		}
	}
	return nil
}

func runTags(c *ctx, _ []string) error {
	cl, err := c.client(false)
	if err != nil {
		return err
	}
	tags, err := cl.Tags(c.ctx())
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(tags)
	}
	if len(tags) == 0 {
		c.printf("no tags\n")
	}
	for _, t := range tags {
		c.printf("%-24s %s\n", t.Tag, plural(t.Threads, "thread", "threads"))
	}
	return nil
}

func runUnread(c *ctx, _ []string) error {
	var since time.Duration
	if c.o.set["since"] {
		var err error
		if since, err = post.ParseSince(c.o.since); err != nil {
			return usageError{err.Error()}
		}
	}
	req := post.UnreadRequest{Peek: c.o.peek, Since: int64(since / time.Second), ToMe: c.o.toMe}
	if c.o.set["thread"] {
		var err error
		if req.Thread, err = c.id(c.o.thread); err != nil {
			return err
		}
	}
	cl, err := c.client(true)
	if err != nil {
		return err
	}
	u, err := cl.Unread(c.ctx(), req)
	if err != nil {
		return err
	}
	if c.o.set["since"] && !u.NewReader {
		c.warnf("--since counts only for a new reader, and this one has read before: agora list and agora search find earlier posts")
	}
	if c.o.json {
		return c.printJSON(u)
	}
	if len(u.Posts) == 0 && len(u.Edited) == 0 {
		c.printf("no unread posts\n")
	}
	for _, p := range u.Posts {
		context := "new thread"
		if !p.IsRoot() {
			context = fmt.Sprintf("in thread #%d", p.Thread)
		}
		c.printPost(p, false, forMe(context, p, cl.Who))
	}
	c.printEdited(u.Edited, cl.Who)
	return nil
}
