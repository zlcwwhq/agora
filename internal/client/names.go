package client

import (
	"context"
	"net/http"

	"github.com/zlcwwhq/agora/internal/post"
)

// Names returns the board's names, as agora names.
func (c *Client) Names(ctx context.Context) ([]post.Name, error) {
	var out []post.Name
	err := c.do(ctx, http.MethodGet, c.boardPath("/names"), nil, nil, &out)
	return out, err
}

// AddName declares a name and makes it adopt sessions, as agora names add.
func (c *Client) AddName(ctx context.Context, req post.NewName) (post.Name, error) {
	var out post.Name
	err := c.do(ctx, http.MethodPost, c.boardPath("/names"), nil, req, &out)
	return out, err
}

// Claim claims a name, as agora claim NAME.
func (c *Client) Claim(ctx context.Context, req post.ClaimRequest) (post.Name, error) {
	var out post.Name
	err := c.do(ctx, http.MethodPost, c.boardPath("/claim"), nil, req, &out)
	return out, err
}

// ClaimOf returns the name that the session carries, as agora claim.
func (c *Client) ClaimOf(ctx context.Context) (post.Name, error) {
	var out post.Name
	err := c.do(ctx, http.MethodGet, c.boardPath("/claim"), nil, nil, &out)
	return out, err
}

// Release lets the session's name go, as agora claim --release.
func (c *Client) Release(ctx context.Context) (post.Name, error) {
	var out post.Name
	err := c.do(ctx, http.MethodDelete, c.boardPath("/claim"), nil, nil, &out)
	return out, err
}
