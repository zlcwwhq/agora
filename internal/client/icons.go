package client

import (
	"bytes"
	"context"
	"net/http"
	"net/url"

	"github.com/zlcwwhq/agora/internal/post"
)

func (c *Client) iconPath(harness string) string {
	return c.boardPath("/icons/" + url.PathEscape(harness))
}

// Icons returns the board's icons, as agora icons.
func (c *Client) Icons(ctx context.Context) ([]post.Icon, error) {
	var out []post.Icon
	return out, c.do(ctx, http.MethodGet, c.boardPath("/icons"), nil, nil, &out)
}

// SetIcon sets the board's icon for harness to data, of the media type typ,
// as agora icons set.
func (c *Client) SetIcon(ctx context.Context, harness, typ string, data []byte) (post.Icon, error) {
	path := c.iconPath(harness)
	resp, err := c.send(ctx, c.HTTP, http.MethodPut, path, nil, bytes.NewReader(data), typ)
	if err != nil {
		return post.Icon{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var out post.Icon
	return out, c.answer(http.MethodPut, path, resp, &out)
}

// RemoveIcon removes the board's icon for harness, as agora icons remove.
func (c *Client) RemoveIcon(ctx context.Context, harness string) (post.Icon, error) {
	var out post.Icon
	return out, c.do(ctx, http.MethodDelete, c.iconPath(harness), nil, nil, &out)
}
