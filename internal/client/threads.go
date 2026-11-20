package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/zlcwwhq/agora/internal/post"
)

// ChangeThread changes the thread that contains id, as agora thread.
func (c *Client) ChangeThread(ctx context.Context, id int64, ch post.ThreadChange) (post.Thread, error) {
	var out post.Thread
	return out, c.do(ctx, http.MethodPatch, c.boardPath("/threads/"+strconv.FormatInt(id, 10)), nil, ch, &out)
}

// Move moves id, as agora move.
func (c *Client) Move(ctx context.Context, id int64, m post.Move) (post.Moved, error) {
	var out post.Moved
	return out, c.do(ctx, http.MethodPost, c.postPath(id)+"/move", nil, m, &out)
}

func (c *Client) attachmentPath(id int64, name string) string {
	return c.postPath(id) + "/attachments/" + url.PathEscape(name)
}

// Attach attaches data to id as name, with the media type typ, as
// agora edit -a.
func (c *Client) Attach(ctx context.Context, id int64, name, typ string, data []byte) (post.Attachment, error) {
	path := c.attachmentPath(id, name)
	resp, err := c.send(ctx, c.HTTP, http.MethodPut, path, nil, bytes.NewReader(data), typ)
	if err != nil {
		return post.Attachment{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var out post.Attachment
	return out, c.answer(http.MethodPut, path, resp, &out)
}

// Detach removes id's attachment name, as agora edit --detach.
func (c *Client) Detach(ctx context.Context, id int64, name string) (post.Attachment, error) {
	var out post.Attachment
	return out, c.do(ctx, http.MethodDelete, c.attachmentPath(id, name), nil, nil, &out)
}

// Download writes id's attachment name to w, as agora download, and returns
// its size.
func (c *Client) Download(ctx context.Context, id int64, name string, w io.Writer) (int64, error) {
	return c.fetch(ctx, c.HTTP, c.attachmentPath(id, name), w)
}

// Backup writes a copy of the server's database to w, as agora backup, and
// returns its size. It may take longer than other calls, so it has no
// timeout of its own.
func (c *Client) Backup(ctx context.Context, w io.Writer) (int64, error) {
	hc := *c.HTTP
	hc.Timeout = 0
	return c.fetch(ctx, &hc, "/api/backup", w)
}

// fetch writes the content that a GET of path answers, which isn't JSON, to
// w, and returns its size. A body shorter than the answer's length is an
// error.
func (c *Client) fetch(ctx context.Context, hc *http.Client, path string, w io.Writer) (int64, error) {
	resp, err := c.send(ctx, hc, http.MethodGet, path, nil, nil, "")
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		if err := c.answer(http.MethodGet, path, resp, nil); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("%s: GET %s got status %d", c.Base, path, resp.StatusCode)
	}
	n, err := io.Copy(w, resp.Body)
	if err != nil {
		return n, fmt.Errorf("%s: GET %s: %w", c.Base, path, err)
	}
	return n, nil
}
