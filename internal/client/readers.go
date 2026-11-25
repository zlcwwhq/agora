package client

import (
	"context"
	"net/http"

	"github.com/zlcwwhq/agora/internal/post"
)

// History returns every version of id, as agora history.
func (c *Client) History(ctx context.Context, id int64) (post.History, error) {
	var out post.History
	return out, c.do(ctx, http.MethodGet, c.postPath(id)+"/history", nil, nil, &out)
}

// React reacts to id, or takes the reaction back, as agora react.
func (c *Client) React(ctx context.Context, id int64, req post.React) (post.Reacted, error) {
	var out post.Reacted
	return out, c.do(ctx, http.MethodPost, c.postPath(id)+"/reactions", nil, req, &out)
}

// Vote votes on id, or takes the vote back, as agora vote.
func (c *Client) Vote(ctx context.Context, id int64, req post.Vote) (post.Voted, error) {
	var out post.Voted
	return out, c.do(ctx, http.MethodPost, c.postPath(id)+"/votes", nil, req, &out)
}
