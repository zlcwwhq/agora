package server

import (
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/zlcwwhq/agora/internal/post"
)

func (p *pages) routeIcons(mux *http.ServeMux) {
	icons := handlers{http.MethodGet: p.inBoard(p.icons)}
	if p.forms {
		icons[http.MethodPost] = withFiles(p.inBoard(p.setIcon))
		mux.HandleFunc("/b/{board}/icons/remove", p.handle(handlers{http.MethodPost: p.inBoard(p.removeIcon)}))
	}
	mux.HandleFunc("/b/{board}/icons", p.handle(icons))
	mux.HandleFunc("/b/{board}/i/{harness}", sandboxed(p.handle(handlers{http.MethodGet: p.inBoard(p.iconContent)})))
}

// iconRow is a harness as the icons page shows it.
type iconRow struct {
	Harness string
	Icon    template.HTML
	// Own says whether the board has an icon of its own for the harness, of
	// the media type Type and the size Size.
	Own        bool
	Type, Size string
	// Posts is the number of the harness's posts on the board.
	Posts int
}

// iconForm is what the form that sets an icon held, and why it was refused.
type iconForm struct {
	Harness, Reason string
}

// iconsView is the data of /b/BOARD/icons.
type iconsView struct {
	base
	Icons []iconRow
	Form  iconForm
}

func (p *pages) icons(w http.ResponseWriter, r *http.Request, b *post.Board, _ form) {
	p.showIcons(w, r, b, http.StatusOK, iconForm{})
}

// showIcons shows the board's harnesses and icons, with the form that sets
// one holding f.
func (p *pages) showIcons(w http.ResponseWriter, r *http.Request, b *post.Board, status int, f iconForm) {
	typed := form{Title: f.Harness}
	is, err := p.s.Store.Icons(r.Context(), b.ID)
	if err != nil {
		p.boardFailed(w, r, b, err, typed)
		return
	}
	hs, err := p.s.Store.Harnesses(r.Context(), b.ID)
	if err != nil {
		p.boardFailed(w, r, b, err, typed)
		return
	}
	v := iconsView{base: p.base(b, "icons", "Icons", query{}), Form: f}
	bi := &boardIcons{board: b.ID, has: map[string]bool{}}
	rows := map[string]*iconRow{}
	for _, h := range hs {
		rows[h.Harness] = &iconRow{Harness: h.Harness, Posts: h.Posts}
	}
	for _, i := range is {
		row := rows[i.Harness]
		if row == nil {
			row = &iconRow{Harness: i.Harness}
			rows[i.Harness] = row
		}
		row.Own, row.Type, row.Size = true, i.Type, post.FormatSize(i.Size)
		bi.has[i.Harness] = true
	}
	for _, row := range rows {
		row.Icon = bi.icon(row.Harness)
		v.Icons = append(v.Icons, *row)
	}
	slices.SortFunc(v.Icons, func(a, b iconRow) int { return strings.Compare(a.Harness, b.Harness) })
	v.icons = bi
	p.render(w, r, status, "icons", v)
}

func (p *pages) setIcon(w http.ResponseWriter, r *http.Request, b *post.Board, f form) {
	typed := iconForm{Harness: strings.TrimSpace(r.PostForm.Get("harness"))}
	var err error
	if f.icon == nil {
		err = post.Errorf(post.Invalid, "choose an image for the icon")
	} else {
		_, _, err = p.s.Store.SetIcon(r.Context(), p.s.Person, b.ID, typed.Harness, f.icon.data)
	}
	if post.KindOf(err) == post.Invalid {
		typed.Reason = sentence(err.Error())
		if f.icon != nil {
			typed.Reason += " Choose the image again."
		}
		p.showIcons(w, r, b, http.StatusBadRequest, typed)
		return
	}
	if err != nil {
		p.boardFailed(w, r, b, err, form{Title: typed.Harness})
		return
	}
	http.Redirect(w, r, boardURL(b.ID, "icons"), http.StatusSeeOther)
}

func (p *pages) removeIcon(w http.ResponseWriter, r *http.Request, b *post.Board, _ form) {
	harness := r.PostForm.Get("harness")
	_, err := p.s.Store.RemoveIcon(r.Context(), p.s.Person, b.ID, harness)
	if post.KindOf(err) == post.Invalid {
		p.showIcons(w, r, b, http.StatusBadRequest, iconForm{Reason: sentence(err.Error())})
		return
	}
	if err != nil {
		p.boardFailed(w, r, b, err, form{})
		return
	}
	http.Redirect(w, r, boardURL(b.ID, "icons"), http.StatusSeeOther)
}

// iconContent serves the board's icon for a harness. Browsers keep it, and
// ask whether it changed whenever a page shows it.
func (p *pages) iconContent(w http.ResponseWriter, r *http.Request, b *post.Board, _ form) {
	harness := r.PathValue("harness")
	if err := post.CheckHarness(harness); err != nil {
		p.failIn(w, r, b, http.StatusBadRequest, sentence(err.Error()), form{})
		return
	}
	i, data, err := p.s.Store.Icon(r.Context(), b.ID, harness)
	if err != nil {
		p.boardFailed(w, r, b, err, form{})
		return
	}
	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", i.Type)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
