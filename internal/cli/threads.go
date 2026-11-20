package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/zlcwwhq/agora/internal/client"
	"github.com/zlcwwhq/agora/internal/post"
)

// threadOpts are the flags of threads' states, pages, attachments, moves,
// and backups.
type threadOpts struct {
	resolve, reopen bool
	pin, unpin      bool
	lock, unlock    bool
	state           string
	skip            int
	attachFiles     stringList
	detach          stringList
	moveTo          string
	output          string
}

var threadCommand = command{
	name: "thread", args: "ID [--resolve | --reopen] [--pin | --unpin] [--lock | --unlock]",
	summary: "show a thread's state, or resolve, pin, or lock it", run: runThread, positional: 1,
	flags: func(fs *flag.FlagSet, o *opts) {
		fs.BoolVar(&o.resolve, "resolve", false, "resolve the thread, as when its question is answered")
		fs.BoolVar(&o.reopen, "reopen", false, "open the thread again")
		fs.BoolVar(&o.pin, "pin", false, "pin the thread before the others; only people may")
		fs.BoolVar(&o.unpin, "unpin", false, "let the thread go")
		fs.BoolVar(&o.lock, "lock", false, "let only people write in the thread; only people may")
		fs.BoolVar(&o.unlock, "unlock", false, "let everyone write in the thread again")
	},
}

var moveCommand = command{
	name: "move", args: "ID (--to TARGET | --title TITLE)",
	summary: "move a post and its replies to another thread, or to a new one; only people may", run: runMove, positional: 1,
	flags: func(fs *flag.FlagSet, o *opts) {
		fs.StringVar(&o.moveTo, "to", "", "move the post into `TARGET`'s thread, replying to it")
		fs.StringVar(&o.title, "title", "", "make the post, a reply, the first of a new thread with this `title`")
	},
}

var downloadCommand = command{
	name: "download", args: "ID NAME [-o FILE] [--force]",
	summary: "save a post's attachment", run: runDownload, positional: 2,
	flags: func(fs *flag.FlagSet, o *opts) {
		fs.StringVar(&o.output, "o", "", "write it to `FILE`, default NAME in the working directory; - is standard output")
		fs.BoolVar(&o.force, "force", false, "replace an existing file")
	},
}

var backupCommand = command{
	name: "backup", args: "FILE [--force]",
	summary: "save a copy of the server's database, with every board", run: runBackup, positional: 1,
	flags: func(fs *flag.FlagSet, o *opts) {
		fs.BoolVar(&o.force, "force", false, "replace an existing file")
	},
}

func attachFlags(fs *flag.FlagSet, o *opts) {
	fs.Var(&o.attachFiles, "a", "attach `FILE`, under its base name, repeatable")
}

func pageFlags(fs *flag.FlagSet, o *opts) {
	fs.StringVar(&o.state, "state", "", "only threads in `STATE`, open or resolved")
	fs.IntVar(&o.skip, "skip", 0, "leave out the first `K` threads")
}

// checkPage checks --state and --skip.
func (c *ctx) checkPage() error {
	if c.o.state != "" {
		if err := post.CheckState(c.o.state); err != nil {
			return usageError{"--state: " + err.Error()}
		}
	}
	if c.o.skip < 0 {
		return usagef("--skip must be at least 0")
	}
	return nil
}

// threadFlags describes what sets a thread apart: resolved, pinned, locked.
func threadFlags(t post.Thread) []string {
	var out []string
	if t.State == post.StateResolved {
		out = append(out, "resolved")
	}
	if t.Pinned {
		out = append(out, "pinned")
	}
	if t.Locked {
		out = append(out, "locked")
	}
	return out
}

func (c *ctx) printAttachments(p post.Post) {
	for _, a := range p.Attachments {
		c.printf("attachment %s (%s, %s)\n", a.Name, a.Type, post.FormatSize(a.Size))
	}
}

// choice returns the change that a pair of flags asks for, such as --pin
// and --unpin, or nil for neither.
func choice(on, off bool, onFlag, offFlag string) (*bool, error) {
	switch {
	case on && off:
		return nil, usagef("%s and %s exclude each other", onFlag, offFlag)
	case on || off:
		return &on, nil
	}
	return nil, nil
}

func runThread(c *ctx, args []string) error {
	id, err := c.id(args[0])
	if err != nil {
		return err
	}
	var ch post.ThreadChange
	if reopen, err := choice(c.o.reopen, c.o.resolve, "--reopen", "--resolve"); err != nil {
		return err
	} else if reopen != nil {
		state := post.StateResolved
		if *reopen {
			state = post.StateOpen
		}
		ch.State = &state
	}
	if ch.Pinned, err = choice(c.o.pin, c.o.unpin, "--pin", "--unpin"); err != nil {
		return err
	}
	if ch.Locked, err = choice(c.o.lock, c.o.unlock, "--lock", "--unlock"); err != nil {
		return err
	}
	// Without changes, it only reads, as anyone may.
	cl, err := c.client(!ch.Empty())
	if err != nil {
		return err
	}
	var t post.Thread
	if ch.Empty() {
		show, err := cl.Show(c.ctx(), id)
		if err != nil {
			return err
		}
		t = show.Thread
	} else if t, err = cl.ChangeThread(c.ctx(), id, ch); err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(t)
	}
	c.printf("#%d %s\n", t.ID, t.Title)
	flags := threadFlags(t)
	if t.State == post.StateOpen {
		flags = append([]string{post.StateOpen}, flags...)
	}
	c.printf("%s · %s · %s\n", strings.Join(flags, " · "), t.Author, plural(t.Replies, "reply", "replies"))
	return nil
}

func runMove(c *ctx, args []string) error {
	id, err := c.id(args[0])
	if err != nil {
		return err
	}
	var m post.Move
	switch {
	case c.o.set["to"] == c.o.set["title"]:
		return usagef("move needs exactly one of --to TARGET and --title TITLE")
	case c.o.set["to"]:
		if m.To, err = c.id(c.o.moveTo); err != nil {
			return err
		}
	default:
		m.Title = &c.o.title
	}
	cl, err := c.client(true)
	if err != nil {
		return err
	}
	moved, err := cl.Move(c.ctx(), id, m)
	if err != nil {
		return err
	}
	if c.o.json {
		return c.printJSON(moved)
	}
	ids := make([]string, len(moved.Moved))
	for i, n := range moved.Moved {
		ids[i] = fmt.Sprintf("#%d", n)
	}
	c.printf("moved %s to thread #%d  %s\n", strings.Join(ids, ", "), moved.Thread, c.postURL(moved.Thread, id))
	return nil
}

// path resolves a path that a command names against the working directory.
func (c *ctx) path(p string) string {
	if filepath.IsAbs(p) || c.env.Dir == "" {
		return p
	}
	return filepath.Join(c.env.Dir, p)
}

// upload is a file to attach.
type upload struct {
	// arg is the file as -a named it, path where it is, and name its
	// attachment's name.
	arg, path, name, typ string
}

// uploads checks the files of -a, before anything is written: that each can
// be read, and that its name and size are an attachment's. A post's
// attachments have different names, so two files mustn't share theirs.
func (c *ctx) uploads() ([]upload, error) {
	var out []upload
	named := map[string]string{}
	for _, arg := range c.o.attachFiles {
		u := upload{arg: arg, path: c.path(arg), name: filepath.Base(arg)}
		if err := post.CheckAttachmentName(u.name); err != nil {
			return nil, usageError{"-a " + arg + ": " + err.Error()}
		}
		if other, ok := named[u.name]; ok {
			return nil, usagef("-a %s and -a %s would both be attached as %s: a post's attachments have different names", other, arg, u.name)
		}
		named[u.name] = arg
		f, err := os.Open(u.path)
		if err != nil {
			return nil, fmt.Errorf("-a: %w", err)
		}
		info, err := f.Stat()
		_ = f.Close()
		switch {
		case err != nil:
			return nil, fmt.Errorf("-a: %w", err)
		case !info.Mode().IsRegular():
			return nil, fmt.Errorf("-a %s: not a file", arg)
		}
		if err := post.CheckAttachmentSize(arg, info.Size()); err != nil {
			return nil, err
		}
		if u.typ = mime.TypeByExtension(filepath.Ext(u.name)); u.typ == "" {
			u.typ = "application/octet-stream"
		}
		out = append(out, u)
	}
	for _, name := range c.o.detach {
		if err := post.CheckAttachmentName(name); err != nil {
			return nil, usageError{"--detach: " + err.Error()}
		}
	}
	return out, nil
}

// attach attaches files to post id, one by one, after the command wrote
// it as verb says, if it wrote it. If one fails, the error names the post
// and the files that weren't attached.
func (c *ctx) attach(cl *client.Client, verb string, id int64, files []upload) error {
	for i, u := range files {
		if err := c.attachFile(cl, id, u); err != nil {
			return fmt.Errorf("%s couldn't attach %s: %w\nnot attached: %s; agora edit %d -a FILE attaches them",
				writtenPrefix(verb, id), u.arg, err, fileArgs(files[i:]), id)
		}
	}
	return nil
}

// writtenPrefix says which post a command wrote, as verb says, if it wrote it,
// before what it then couldn't do.
func writtenPrefix(verb string, id int64) string {
	if verb == "" {
		return fmt.Sprintf("#%d:", id)
	}
	return fmt.Sprintf("%s #%d, but", verb, id)
}

// fileArgs returns files as -a named them.
func fileArgs(files []upload) string {
	out := make([]string, len(files))
	for i, u := range files {
		out[i] = u.arg
	}
	return strings.Join(out, ", ")
}

func (c *ctx) attachFile(cl *client.Client, id int64, u upload) error {
	f, err := os.Open(u.path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, post.MaxAttachment+1))
	if err != nil {
		return err
	}
	if err := post.CheckAttachmentSize(u.arg, int64(len(data))); err != nil {
		return err
	}
	_, err = cl.Attach(c.ctx(), id, u.name, u.typ, data)
	return err
}

// edit edits post id's fields that e sets, and returns the post. An edit
// with none, which only attaches or detaches, isn't an edit.
func (c *ctx) edit(cl *client.Client, id int64, e post.Edit) (post.Post, error) {
	if !e.Empty() {
		return cl.Edit(c.ctx(), id, e)
	}
	show, err := cl.Show(c.ctx(), id)
	if err != nil {
		return post.Post{}, err
	}
	for _, p := range show.Posts {
		if p.ID == id {
			return p, nil
		}
	}
	return post.Post{}, post.NoPost(cl.Board, id)
}

// changeAttachments detaches what --detach names from post id, then
// attaches files, after the command edited it, if it did.
func (c *ctx) changeAttachments(cl *client.Client, id int64, edited bool, files []upload) error {
	verb := ""
	if edited {
		verb = "edited"
	}
	for _, name := range c.o.detach {
		if _, err := cl.Detach(c.ctx(), id, name); err != nil {
			msg := fmt.Sprintf("%s couldn't detach %s: %v", writtenPrefix(verb, id), name, err)
			if len(files) > 0 {
				msg += "\nnot attached: " + fileArgs(files)
			}
			return post.Errorf(post.KindOf(err), "%s", msg)
		}
	}
	return c.attach(cl, verb, id, files)
}

// checkTarget fails if path exists, unless force lets a file be replaced;
// a directory never is.
func checkTarget(path string, force bool) error {
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		return fmt.Errorf("%s is a directory: agora never replaces one", path)
	case err == nil && !force:
		return fmt.Errorf("%s exists: pass --force to replace it", path)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return err
	}
	return nil
}

// writeFile writes what write writes to path, with the permissions perm,
// through a temporary file in its directory that replaces path only once it
// is complete, so that a failure leaves no path behind. It fails as
// checkTarget does.
func writeFile(path string, force bool, perm os.FileMode, write func(io.Writer) (int64, error)) (int64, error) {
	if err := checkTarget(path, force); err != nil {
		return 0, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return 0, err
	}
	n, err := write(f)
	if err == nil {
		err = f.Chmod(perm)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		// Again, since the file may have appeared meanwhile.
		err = checkTarget(path, force)
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return 0, err
	}
	return n, nil
}

// printWritten prints the file that a command wrote and its size.
func (c *ctx) printWritten(path string, size int64) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if c.o.json {
		return c.printJSON(struct {
			File string `json:"file"`
			Size int64  `json:"size"`
		}{abs, size})
	}
	c.printf("wrote %s (%s)\n", abs, post.FormatSize(size))
	return nil
}

func runDownload(c *ctx, args []string) error {
	id, err := c.id(args[0])
	if err != nil {
		return err
	}
	name := args[1]
	if err := post.CheckAttachmentName(name); err != nil {
		return usageError{err.Error()}
	}
	out := name
	if c.o.set["o"] {
		out = c.o.output
	}
	switch {
	case out == "":
		return usagef("-o needs a file, or - for standard output")
	case out == "-" && c.o.json:
		return usagef("-o - writes the attachment to standard output, so --json can't")
	}
	cl, err := c.client(false)
	if err != nil {
		return err
	}
	if out == "-" {
		var buf bytes.Buffer
		if _, err := cl.Download(c.ctx(), id, name, &buf); err != nil {
			return err
		}
		_, err := c.stdout.Write(buf.Bytes())
		return err
	}
	path := c.path(out)
	n, err := writeFile(path, c.o.force, 0o644, func(w io.Writer) (int64, error) { return cl.Download(c.ctx(), id, name, w) })
	if err != nil {
		return err
	}
	return c.printWritten(path, n)
}

func runBackup(c *ctx, args []string) error {
	if args[0] == "" {
		return usagef("usage: agora backup FILE [--force]")
	}
	cl, err := c.serverClient(true)
	if err != nil {
		return err
	}
	path := c.path(args[0])
	n, err := writeFile(path, c.o.force, 0o600, func(w io.Writer) (int64, error) { return cl.Backup(c.ctx(), w) })
	if err != nil {
		return err
	}
	return c.printWritten(path, n)
}
