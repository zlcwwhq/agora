package server

import (
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/zlcwwhq/agora/internal/post"
)

// parsePage reads the parameters of a page of threads, state and skip, into
// q, returning a message for the page if one is invalid.
func (q *query) parsePage(v url.Values) string {
	if q.State = v.Get("state"); q.State != "" && post.CheckState(q.State) != nil {
		return fmt.Sprintf("Show open or resolved threads, not %q.", q.State)
	}
	if s := v.Get("skip"); s != "" {
		var err error
		if q.Skip, err = strconv.Atoi(s); err != nil || q.Skip < 0 {
			return fmt.Sprintf("Skip at least 0 threads, not %q.", s)
		}
	}
	return ""
}

// pages returns the addresses of the pages after and before q's, given that
// q's page found found threads when it asked for one more than it shows.
func (q query) pages(found int) (next, prev string) {
	if found > q.N {
		r := q
		r.Skip = q.Skip + q.N
		next = r.URL()
	}
	if q.Skip > 0 {
		r := q
		r.Skip = max(q.Skip-q.N, 0)
		prev = r.URL()
	}
	return next, prev
}

// WithState returns the address of q's threads in state, or in every state
// if it is empty.
func (q query) WithState(state string) string {
	r := q
	r.N, r.Skip, r.State = 0, 0, state
	return r.URL()
}

// upload is a file that a form sent.
type upload struct {
	name, typ string
	data      []byte
}

// moveForm is what the form that moves a post held, and why it was refused.
type moveForm struct {
	To, Title, Reason string
}

// fileFields are what the forms of posts hold besides their text: the
// files to attach, the attachments to detach, and what the form that moves
// the post held.
type fileFields struct {
	files  []upload
	detach []string
	move   moveForm
}

// withFiles lets a form that carries files, sent as multipart/form-data, be
// larger than other requests.
func withFiles(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isMultipart(r) {
			allowBody(r, maxFilesForm)
		}
		h(w, r)
	}
}

func isMultipart(r *http.Request) bool {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return mt == "multipart/form-data"
}

// parseForm parses a form, sent as multipart/form-data or not, with its
// files in memory.
func parseForm(r *http.Request) error {
	if !isMultipart(r) {
		return r.ParseForm()
	}
	if err := r.ParseMultipartForm(maxFilesForm); err != nil {
		return err
	}
	// What a form types is bounded as other requests are.
	n := 0
	for _, vs := range r.PostForm {
		for _, v := range vs {
			n += len(v)
		}
	}
	if n > maxBody {
		return &http.MaxBytesError{Limit: maxBody}
	}
	return nil
}

// readFiles reads the files that a form sent, and the names of the
// attachments that it detaches, into typed, answering the request, with
// what was typed, if one isn't allowed.
func (p *pages) readFiles(w http.ResponseWriter, r *http.Request, typed form) (form, bool) {
	typed.detach = r.PostForm["detach"]
	if r.MultipartForm == nil {
		return typed, true
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	named := map[string]bool{}
	for _, fh := range r.MultipartForm.File["files"] {
		if fh.Filename == "" && fh.Size == 0 {
			// A file field that holds no file.
			continue
		}
		if fh.Size > post.MaxAttachment {
			p.failIn(w, r, nil, http.StatusRequestEntityTooLarge, fmt.Sprintf("%s is larger than 10 MiB, which is more than an attachment may have.", fh.Filename), typed)
			return form{}, false
		}
		reason := ""
		if err := post.CheckAttachmentName(fh.Filename); err != nil {
			reason = sentence(err.Error())
		} else if named[fh.Filename] {
			reason = fmt.Sprintf("Two files are named %s, but a post's attachments have different names.", fh.Filename)
		}
		if reason != "" {
			p.failIn(w, r, nil, http.StatusBadRequest, reason, typed)
			return form{}, false
		}
		named[fh.Filename] = true
		data, err := readUpload(fh)
		if err != nil {
			p.failIn(w, r, nil, http.StatusBadRequest, "Can't read the form: "+err.Error(), typed)
			return form{}, false
		}
		typ := mime.TypeByExtension(path.Ext(fh.Filename))
		if typ == "" {
			typ = "application/octet-stream"
		}
		typed.files = append(typed.files, upload{name: fh.Filename, typ: typ, data: data})
	}
	return typed, true
}

func readUpload(fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// attachFiles attaches the files of a form to post id, which it wrote as
// verb says.
func (p *pages) attachFiles(r *http.Request, board, verb string, id int64, f form) error {
	for _, u := range f.files {
		if _, _, err := p.s.Store.Attach(r.Context(), p.s.Person, board, id, u.name, u.typ, u.data); err != nil {
			return fmt.Errorf("%s #%d, but couldn't attach %s: %w", verb, id, u.name, err)
		}
	}
	return nil
}

// changeFiles detaches the attachments of post id that the edit form
// checked, then attaches its files.
func (p *pages) changeFiles(r *http.Request, board string, id int64, f form) error {
	for _, name := range f.detach {
		if _, err := p.s.Store.Detach(r.Context(), p.s.Person, board, id, name); err != nil {
			return fmt.Errorf("edited #%d, but couldn't detach %s: %w", id, name, err)
		}
	}
	return p.attachFiles(r, board, "edited", id, f)
}

func (p *pages) routeThreadForms(mux *http.ServeMux) {
	mux.HandleFunc("/b/{board}/t/{id}/thread", p.handle(handlers{http.MethodPost: p.inBoard(p.changeThread)}))
	mux.HandleFunc("/b/{board}/t/{id}/move", p.handle(handlers{http.MethodPost: p.inBoard(p.move)}))
}

// threadChanges are the changes that the buttons of a thread make.
var threadChanges = func() map[string]post.ThreadChange {
	open, resolved, yes, no := post.StateOpen, post.StateResolved, true, false
	return map[string]post.ThreadChange{
		"resolve": {State: &resolved},
		"reopen":  {State: &open},
		"pin":     {Pinned: &yes},
		"unpin":   {Pinned: &no},
		"lock":    {Locked: &yes},
		"unlock":  {Locked: &no},
	}
}()

func (p *pages) changeThread(w http.ResponseWriter, r *http.Request, b *post.Board, f form) {
	id, ok := p.pathID(w, r, b, f)
	if !ok {
		return
	}
	change := r.PostForm.Get("change")
	c, ok := threadChanges[change]
	if !ok {
		p.failIn(w, r, b, http.StatusBadRequest, fmt.Sprintf("A thread is resolved, reopened, pinned, unpinned, locked, or unlocked, not %q.", change), f)
		return
	}
	t, err := p.s.Store.ChangeThread(r.Context(), p.s.Person, b.ID, id, c)
	if err != nil {
		p.boardFailed(w, r, b, err, f)
		return
	}
	http.Redirect(w, r, inThread(b.ID, t.ID, t.ID), http.StatusSeeOther)
}

func (p *pages) move(w http.ResponseWriter, r *http.Request, b *post.Board, f form) {
	id, ok := p.pathID(w, r, b, f)
	if !ok {
		return
	}
	mf := moveForm{To: strings.TrimSpace(r.PostForm.Get("to")), Title: r.PostForm.Get("title")}
	var m post.Move
	var err error
	if mf.To != "" {
		m.To, err = post.ParseID(mf.To)
	}
	if _, ok := r.PostForm["title"]; ok {
		m.Title = &mf.Title
	}
	var moved post.Moved
	if err == nil {
		moved, err = p.s.Store.Move(r.Context(), p.s.Person, b.ID, id, m)
	}
	if post.KindOf(err) == post.Invalid {
		cur, gerr := p.s.Store.Get(r.Context(), b.ID, id)
		if gerr != nil {
			p.boardFailed(w, r, b, gerr, f)
			return
		}
		mf.Reason = sentence(err.Error())
		edit := form{Title: cur.Title, Tags: strings.Join(cur.Tags, " "), Body: cur.Body, HasTitle: cur.IsRoot()}
		edit.move = mf
		p.showEdit(w, r, b, http.StatusBadRequest, cur, edit)
		return
	}
	if err != nil {
		p.boardFailed(w, r, b, err, f)
		return
	}
	http.Redirect(w, r, inThread(b.ID, moved.Thread, id), http.StatusSeeOther)
}

// attachmentRow is an attachment as a thread's page shows it.
type attachmentRow struct {
	Name, URL, Size string
	// Image says whether the attachment is an image that its address shows.
	Image bool
}

// attachmentURL is the address of post id's attachment name.
func attachmentURL(board string, id int64, name string) string {
	return fmt.Sprintf("/b/%s/a/%d/%s", board, id, url.PathEscape(name))
}

func attachmentRows(board string, p post.Post) []attachmentRow {
	rows := make([]attachmentRow, 0, len(p.Attachments))
	for _, a := range p.Attachments {
		t, _ := inlineType(a.Type)
		rows = append(rows, attachmentRow{
			Name: a.Name, URL: attachmentURL(board, p.ID, a.Name), Size: post.FormatSize(a.Size),
			Image: strings.HasPrefix(t, "image/"),
		})
	}
	return rows
}

// inlineType returns the type with which /b/BOARD/a/ID/NAME shows an
// attachment of the media type typ in the browser, and whether it does:
// only images and plain text are, never as a page.
func inlineType(typ string) (string, bool) {
	mt, _, err := mime.ParseMediaType(typ)
	switch {
	case err != nil:
	case mt == "image/png", mt == "image/jpeg", mt == "image/gif", mt == "image/webp":
		return mt, true
	case mt == "text/plain":
		return "text/plain; charset=utf-8", true
	}
	return "application/octet-stream", false
}

// sandboxed gives every answer of h the headers of attachments, so that
// none acts as one of the pages.
func sandboxed(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h(&sandboxWriter{ResponseWriter: w}, r)
	}
}

// sandboxWriter sets the headers of attachments when the answer starts.
type sandboxWriter struct {
	http.ResponseWriter
	started bool
}

func (s *sandboxWriter) WriteHeader(code int) {
	if !s.started {
		s.started = true
		h := s.Header()
		h.Set("Content-Security-Policy", "sandbox")
		h.Set("X-Content-Type-Options", "nosniff")
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *sandboxWriter) Write(b []byte) (int, error) {
	if !s.started {
		s.WriteHeader(http.StatusOK)
	}
	return s.ResponseWriter.Write(b)
}

func (p *pages) routeAttachments(mux *http.ServeMux) {
	mux.HandleFunc("/b/{board}/a/{id}/{name}", sandboxed(p.handle(handlers{http.MethodGet: p.inBoard(p.attachment)})))
}

func (p *pages) attachment(w http.ResponseWriter, r *http.Request, b *post.Board, _ form) {
	id, ok := p.pathID(w, r, b, form{})
	if !ok {
		return
	}
	name := r.PathValue("name")
	if err := post.CheckAttachmentName(name); err != nil {
		p.failIn(w, r, b, http.StatusBadRequest, sentence(err.Error()), form{})
		return
	}
	a, data, err := p.s.Store.Attachment(r.Context(), b.ID, id, name)
	if err != nil {
		p.boardFailed(w, r, b, err, form{})
		return
	}
	typ, inline := inlineType(a.Type)
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	if d := mime.FormatMediaType(disposition, map[string]string{"filename": a.Name}); d != "" {
		disposition = d
	}
	h := w.Header()
	h.Set("Content-Type", typ)
	h.Set("Content-Disposition", disposition)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
