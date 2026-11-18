// Package board reads and writes a board's files as api/post-format.md lays
// them out: posts in the synchronized root, and this machine's state outside
// it.
package board

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/zlcwwhq/agora/internal/cursor"
	"github.com/zlcwwhq/agora/internal/post"
)

// ErrNotInitialized means that this machine hasn't run init for the board.
var ErrNotInitialized = errors.New("this machine has no node name for the board: run agora init --node NAME")

// Board is one board as seen from this machine.
type Board struct {
	// Root is the synchronized directory.
	Root string
	// State is this machine's state directory for the board.
	State string
}

// StateHome returns the base of state directories: $XDG_STATE_HOME if it is
// absolute, else ~/.local/state.
func StateHome(getenv func(string) string) (string, error) {
	if s := getenv("XDG_STATE_HOME"); filepath.IsAbs(s) {
		return s, nil
	}
	if home := getenv("HOME"); filepath.IsAbs(home) {
		return filepath.Join(home, ".local", "state"), nil
	}
	return "", errors.New("can't locate the state directory: neither XDG_STATE_HOME nor HOME is an absolute path")
}

// Open returns the board at root, whose state is beneath stateHome. The board
// needn't exist yet.
func Open(root, stateHome string) (*Board, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	resolved, err := resolve(abs)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(resolved))
	return &Board{
		Root:  resolved,
		State: filepath.Join(stateHome, "agora", hex.EncodeToString(sum[:8])),
	}, nil
}

// resolve evaluates the symbolic links of the longest existing prefix of path.
func resolve(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path, nil
	}
	p, err := resolve(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(p, filepath.Base(path)), nil
}

func (b *Board) posts() string              { return filepath.Join(b.Root, "posts") }
func (b *Board) nodeDir(node string) string { return filepath.Join(b.posts(), node) }
func (b *Board) scratch() string            { return filepath.Join(b.Root, ".tmp") }

// IndexPath returns the path of the local index.
func (b *Board) IndexPath() string { return filepath.Join(b.State, "index.db") }

// Node returns this machine's node name, or ErrNotInitialized.
func (b *Board) Node() (string, error) {
	data, err := os.ReadFile(filepath.Join(b.State, "node"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotInitialized
	}
	if err != nil {
		return "", err
	}
	node := strings.TrimSpace(string(data))
	if !post.ValidNode(node) {
		return "", fmt.Errorf("%s holds an invalid node name %q", filepath.Join(b.State, "node"), node)
	}
	return node, nil
}

// Init creates the board's directories and records node as this machine's
// node name. It refuses a name whose directory already holds posts, which
// another machine may be writing, unless resume says they are this machine's
// own. Initializing again with the same name succeeds.
func (b *Board) Init(node string, resume bool) error {
	if !post.ValidNode(node) {
		return fmt.Errorf("invalid node name %q: use 1 to 16 lowercase letters and digits, starting with a letter", node)
	}
	current, err := b.Node()
	switch {
	case err == nil && current != node:
		return fmt.Errorf("this machine is already node %q for %s", current, b.Root)
	case err != nil && !errors.Is(err, ErrNotInitialized):
		return err
	}
	for _, dir := range []string{b.nodeDir(node), b.scratch()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(b.State, 0o700); err != nil {
		return err
	}
	if current == node {
		return nil
	}
	highest, err := b.highest(node)
	if err != nil {
		return err
	}
	if highest > 0 && !resume {
		return fmt.Errorf("posts/%s already holds posts up to %s-%d, so another machine may be using that name: "+
			"choose another name, or pass --resume if they are this machine's own", node, node, highest)
	}
	if err := b.raiseHighwater(highest); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(b.State, "node"), []byte(node+"\n"))
}

// highest returns the highest post number in node's directory.
func (b *Board) highest(node string) (int, error) {
	entries, err := os.ReadDir(b.nodeDir(node))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	highest := 0
	for _, e := range entries {
		if n, ok := post.ParseFileName(e.Name()); ok {
			highest = max(highest, n)
		}
	}
	return highest, nil
}

func (b *Board) highwater() (int, error) {
	data, err := os.ReadFile(filepath.Join(b.State, "highwater"))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", filepath.Join(b.State, "highwater"), err)
	}
	return n, nil
}

func (b *Board) raiseHighwater(n int) error {
	current, err := b.highwater()
	if err != nil {
		return err
	}
	if n <= current {
		return nil
	}
	return writeFileAtomic(filepath.Join(b.State, "highwater"), []byte(strconv.Itoa(n)+"\n"))
}

// Publish writes p as this machine's next post, assigning its id. The file
// appears complete or not at all, and concurrent publishers on this machine
// receive distinct numbers.
func (b *Board) Publish(p post.Post) (post.Post, error) {
	node, err := b.Node()
	if err != nil {
		return post.Post{}, err
	}
	dir := b.nodeDir(node)
	for _, d := range []string{dir, b.scratch()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return post.Post{}, err
		}
	}
	highest, err := b.highest(node)
	if err != nil {
		return post.Post{}, err
	}
	hw, err := b.highwater()
	if err != nil {
		return post.Post{}, err
	}
	n := max(highest, hw) + 1
	for {
		p.ID = post.ID{Node: node, N: n}
		data, err := post.Render(p)
		if err != nil {
			return post.Post{}, err
		}
		tmp, err := b.writeScratch(node, data)
		if err != nil {
			return post.Post{}, err
		}
		err = os.Link(tmp, filepath.Join(dir, post.FileName(n)))
		// The post is published once linked, so failing to remove the scratch
		// file mustn't report failure; a leftover scratch file is harmless.
		_ = os.Remove(tmp)
		if errors.Is(err, fs.ErrExist) {
			n++
			continue
		}
		if err != nil {
			return post.Post{}, fmt.Errorf("publishing %s: %w", p.ID, err)
		}
		// The post is published now. Reporting a failure of these
		// follow-ups would make callers retry and post twice, so they are
		// best effort: without the directory sync, a crash may lose the post,
		// and without the high-water mark, only deleting the post by hand
		// lets its number be reused.
		_ = syncDir(dir)
		_ = b.raiseHighwater(n)
		return p, nil
	}
}

// writeScratch writes data to a new file in the scratch directory and syncs
// it, returning its path.
func (b *Board) writeScratch(node string, data []byte) (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	path := filepath.Join(b.scratch(), node+"-"+hex.EncodeToString(random[:]))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// File is a post file found by Scan.
type File struct {
	// Path is relative to the root, with slashes: posts/<node>/<n>.md.
	Path    string
	ID      post.ID
	Size    int64
	MtimeNs int64
}

// Scan lists the board's post files, ordered by id. Entries that aren't
// named like posts are ignored.
func (b *Board) Scan() ([]File, error) {
	nodes, err := os.ReadDir(b.posts())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []File
	for _, nodeEntry := range nodes {
		node := nodeEntry.Name()
		if !nodeEntry.IsDir() || !post.ValidNode(node) {
			continue
		}
		entries, err := os.ReadDir(b.nodeDir(node))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			n, ok := post.ParseFileName(e.Name())
			if !ok || !e.Type().IsRegular() {
				continue
			}
			info, err := e.Info()
			if errors.Is(err, fs.ErrNotExist) {
				continue // removed since listing
			}
			if err != nil {
				return nil, err
			}
			files = append(files, File{
				Path:    "posts/" + node + "/" + e.Name(),
				ID:      post.ID{Node: node, N: n},
				Size:    info.Size(),
				MtimeNs: info.ModTime().UnixNano(),
			})
		}
	}
	slices.SortFunc(files, func(x, y File) int {
		if x.ID.Less(y.ID) {
			return -1
		}
		if y.ID.Less(x.ID) {
			return 1
		}
		return 0
	})
	return files, nil
}

// Read returns the contents of a file that Scan listed.
func (b *Board) Read(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(b.Root, filepath.FromSlash(path)))
}

func (b *Board) cursorPath(harness, session string) string {
	return filepath.Join(b.State, "cursors", harness, session+".json")
}

// LoadCursor returns the reader's cursor and whether it existed.
func (b *Board) LoadCursor(harness, session string) (*cursor.Cursor, bool, error) {
	data, err := os.ReadFile(b.cursorPath(harness, session))
	if errors.Is(err, fs.ErrNotExist) {
		return cursor.New(), false, nil
	}
	if err != nil {
		return nil, false, err
	}
	c, err := cursor.Parse(data)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", b.cursorPath(harness, session), err)
	}
	return c, true, nil
}

// SaveCursor stores the reader's cursor.
func (b *Board) SaveCursor(harness, session string, c *cursor.Cursor) error {
	path := b.cursorPath(harness, session)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, c.Marshal())
}

// writeFileAtomic replaces path's contents by renaming a synced temporary file
// over it.
func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if closeErr := d.Close(); err == nil {
		err = closeErr
	}
	// Some systems, such as Windows, can't sync directories.
	if errors.Is(err, os.ErrInvalid) || errors.Is(err, errors.ErrUnsupported) {
		return nil
	}
	return err
}
