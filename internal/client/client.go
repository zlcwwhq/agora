// Package client calls an agora server through the HTTP API of
// api/http.md.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
)

// Client calls one server, on one board, as one identity.
type Client struct {
	// Base is the server's address, such as http://127.0.0.1:7777.
	Base string
	// Board is the board of the calls that need one.
	Board string
	// Who is the identity of requests that need one.
	Who  post.Identity
	HTTP *http.Client
}

// New returns a client of the server at base, on board.
func New(base, board string, who post.Identity) *Client {
	return &Client{Base: base, Board: board, Who: who, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// UnreachableError is a server that can't be reached.
type UnreachableError struct {
	Server string
	Err    error
}

func (e *UnreachableError) Error() string {
	return fmt.Sprintf("can't reach the agora server at %s: %v\n"+
		"Start it there with agora serve, or name another one with --server, AGORA_SERVER, or [server] in .agora", e.Server, e.Err)
}

func (e *UnreachableError) Unwrap() error { return e.Err }

// do sends a request and decodes its JSON answer into out.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	u := c.Base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if !c.Who.IsZero() {
		req.Header.Set("Agora-Author", c.Who.Header())
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) && ctx.Err() == nil {
			return &UnreachableError{Server: c.Base, Err: ue.Err}
		}
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("%s: reading the answer: %w", c.Base, err)
	}
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != "application/json" {
		return fmt.Errorf("%s doesn't answer as an agora server: %s %s got status %d", c.Base, method, path, resp.StatusCode)
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(data, &e); err != nil || e.Error == "" {
			return fmt.Errorf("%s: %s %s got status %d", c.Base, method, path, resp.StatusCode)
		}
		return post.Errorf(kind(resp.StatusCode), "%s", e.Error)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: invalid answer to %s %s: %w", c.Base, method, path, err)
	}
	return nil
}

func kind(status int) post.Kind {
	switch status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType:
		return post.Invalid
	case http.StatusForbidden:
		return post.Forbidden
	case http.StatusNotFound:
		return post.NotFound
	case http.StatusConflict:
		return post.Conflict
	}
	return post.Other
}

func filterQuery(f post.Filter) url.Values {
	q := url.Values{}
	for _, t := range f.Tags {
		q.Add("t", t)
	}
	if f.Author != "" {
		q.Set("author", f.Author)
	}
	if f.Limit > 0 {
		q.Set("n", strconv.Itoa(f.Limit))
	}
	return q
}

// boardPath returns the path of the board's endpoint p.
func (c *Client) boardPath(p string) string { return "/api/boards/" + url.PathEscape(c.Board) + p }

func (c *Client) postPath(id int64) string { return c.boardPath("/posts/" + strconv.FormatInt(id, 10)) }

// Boards lists the server's boards, as agora boards.
func (c *Client) Boards(ctx context.Context) ([]post.Board, error) {
	var out []post.Board
	return out, c.do(ctx, http.MethodGet, "/api/boards", nil, nil, &out)
}

// CreateBoard creates a board, as agora boards create.
func (c *Client) CreateBoard(ctx context.Context, req post.NewBoard) (post.Board, error) {
	var out post.Board
	return out, c.do(ctx, http.MethodPost, "/api/boards", nil, req, &out)
}

// List lists threads, as agora list.
func (c *Client) List(ctx context.Context, f post.Filter) ([]post.Thread, error) {
	var out []post.Thread
	return out, c.do(ctx, http.MethodGet, c.boardPath("/threads"), filterQuery(f), nil, &out)
}

// Search searches, as agora search.
func (c *Client) Search(ctx context.Context, q post.Query) ([]post.Result, error) {
	v := filterQuery(q.Filter)
	for _, t := range q.Terms {
		v.Add("q", t)
	}
	if q.Field != "" {
		v.Set("in", q.Field)
	}
	var out []post.Result
	return out, c.do(ctx, http.MethodGet, c.boardPath("/search"), v, nil, &out)
}

// Tags lists tags, as agora tags.
func (c *Client) Tags(ctx context.Context) ([]post.TagCount, error) {
	var out []post.TagCount
	return out, c.do(ctx, http.MethodGet, c.boardPath("/tags"), nil, nil, &out)
}

// Show returns the thread that contains id, as agora show.
func (c *Client) Show(ctx context.Context, id int64) (post.Show, error) {
	var out post.Show
	return out, c.do(ctx, http.MethodGet, c.postPath(id), nil, nil, &out)
}

// Post starts a thread, as agora post.
func (c *Client) Post(ctx context.Context, req post.NewThread) (post.Published, error) {
	var out post.Published
	return out, c.do(ctx, http.MethodPost, c.boardPath("/threads"), nil, req, &out)
}

// Reply replies to id, as agora reply.
func (c *Client) Reply(ctx context.Context, id int64, req post.NewReply) (post.Published, error) {
	var out post.Published
	return out, c.do(ctx, http.MethodPost, c.postPath(id)+"/replies", nil, req, &out)
}

// Edit edits id, as agora edit.
func (c *Client) Edit(ctx context.Context, id int64, e post.Edit) (post.Post, error) {
	var out post.Post
	return out, c.do(ctx, http.MethodPatch, c.postPath(id), nil, e, &out)
}

// Delete deletes id, as agora delete.
func (c *Client) Delete(ctx context.Context, id int64) (post.Removed, error) {
	var out post.Removed
	return out, c.do(ctx, http.MethodDelete, c.postPath(id), nil, nil, &out)
}

// Unread returns the identity's unread posts, as agora unread.
func (c *Client) Unread(ctx context.Context, req post.UnreadRequest) (post.Unread, error) {
	var out post.Unread
	return out, c.do(ctx, http.MethodPost, c.boardPath("/unread"), nil, req, &out)
}
