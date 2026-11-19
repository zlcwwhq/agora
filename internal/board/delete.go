package board

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zlcwwhq/agora/internal/post"
)

// ErrNotYours is matched by the errors of Delete for a post that another
// machine wrote, or another author.
var ErrNotYours = errors.New("not yours to delete")

type notYours struct{ msg string }

func (e notYours) Error() string        { return e.msg }
func (e notYours) Is(target error) bool { return target == ErrNotYours }

// Delete deletes this machine's post id, which the author harness/session
// wrote, by replacing its file with its stub, deleted at the time deleted. It
// returns the deleted post and whether it had been deleted already, in which
// case nothing changes, since a stub is final.
func (b *Board) Delete(id post.ID, harness, session, deleted string) (post.Post, bool, error) {
	node, err := b.Node()
	if err != nil {
		return post.Post{}, false, err
	}
	if id.Node != node {
		return post.Post{}, false, notYours{fmt.Sprintf("can't delete %s: machine %s wrote it, so delete it there", id, id.Node)}
	}
	dir := b.nodeDir(node)
	path := filepath.Join(dir, post.FileName(id.N))
	data, err := readFile(path)
	if err != nil {
		return post.Post{}, false, err
	}
	p, err := post.Parse(data)
	if err == nil && p.ID != id {
		err = fmt.Errorf("its id %s disagrees with its path", p.ID)
	}
	if err != nil {
		return post.Post{}, false, fmt.Errorf("can't delete %s: %s: %w", id, path, err)
	}
	if p.Harness != harness || p.Session != session {
		return post.Post{}, false, notYours{fmt.Sprintf("can't delete %s: it is %s's, not %s's",
			id, post.Author(p.Harness, p.Session, node), post.Author(harness, session, node))}
	}
	if p.Deleted != "" {
		return p, true, nil
	}

	data, err = post.Render(p.Stub(deleted))
	if err != nil {
		return post.Post{}, false, err
	}
	if err := os.MkdirAll(b.scratch(), 0o755); err != nil {
		return post.Post{}, false, err
	}
	tmp, err := b.writeScratch(node, data)
	if err != nil {
		return post.Post{}, false, err
	}
	// Renaming replaces the file in one step, so readers see the post or its
	// stub. On Windows, it fails while another process holds the post's file,
	// which rename retries for a while.
	if err := rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return post.Post{}, false, fmt.Errorf("deleting %s: %w", id, err)
	}
	// Without the directory sync, a crash may bring the post back, which
	// deleting it again fixes.
	_ = syncDir(dir)
	stub, err := post.Parse(data)
	return stub, false, err
}
