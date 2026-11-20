package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/zlcwwhq/agora/internal/post"
)

// The bounds of request bodies besides maxBody: an attachment's, and a
// form's with files, each of whose files has at most post.MaxAttachment.
const (
	maxAttachmentBody = post.MaxAttachment
	maxFilesForm      = 32 << 20
)

// limitedBody bounds a request's body to limit bytes, as http.MaxBytesReader
// does, and lets the handler raise the bound with allowBody before it reads
// the body.
type limitedBody struct {
	w     http.ResponseWriter
	body  io.ReadCloser
	limit int64
	// r reads the body once reading starts.
	r io.ReadCloser
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.r == nil {
		b.r = http.MaxBytesReader(b.w, b.body, b.limit)
	}
	return b.r.Read(p)
}

func (b *limitedBody) Close() error { return b.body.Close() }

// allowBody raises the bound of r's body to limit, unless reading it has
// started.
func allowBody(r *http.Request, limit int64) {
	if b, ok := r.Body.(*limitedBody); ok && b.r == nil {
		b.limit = limit
	}
}

// tooLarge describes the error of a body larger than its bound.
func tooLarge(err error) string {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return fmt.Sprintf("the request is larger than %d MiB", mbe.Limit>>20)
	}
	return "the request is too large"
}
