package server

import (
	"context"
	"testing"

	"github.com/zlcwwhq/agora/internal/post"
)

// png is the start of a PNG image, enough for the pages.
var png = []byte("\x89PNG\r\n\x1a\nfake")

func TestBodyShowsItsAttachmentsAsImages(t *testing.T) {
	s := serve(t, post.Identity{})
	s.thread(codex, "Files", "![the failing run](shot.png)\n\n"+
		"![](<my shot.png>) and ![](my%20shot.png)\n\n"+
		"![图](截图.png)\n\n"+
		"![say \"hi\" & 1<2](shot.png)\n\n"+
		"![notes](notes.txt)\n\n"+
		"![missing](nope.png)\n\n"+
		"![far](https://example.com/x.png) ![proto](//example.com/x.png) ![script](javascript:alert(1))\n\n"+
		"[![inside](shot.png)](https://example.com/page) [![outside](https://example.com/y.png)](https://example.com/page)\n")
	s.attach(codex, 1, "shot.png", "image/png", png)
	s.attach(codex, 1, "my shot.png", "image/png", png)
	s.attach(codex, 1, "截图.png", "image/jpeg; charset=binary", png)
	s.attach(codex, 1, "notes.txt", "text/plain", []byte("plain"))

	r := s.get("/b/b/t/1")
	expect(t, r, 200,
		// The post's own attachments, by name, link to themselves.
		`<a href="/b/b/a/1/shot.png" class="embed"><img src="/b/b/a/1/shot.png" alt="the failing run"></a>`,
		`<a href="/b/b/a/1/my%20shot.png" class="embed"><img src="/b/b/a/1/my%20shot.png" alt=""></a> and `+
			`<a href="/b/b/a/1/my%20shot.png" class="embed"><img src="/b/b/a/1/my%20shot.png" alt=""></a>`,
		`<img src="/b/b/a/1/%E6%88%AA%E5%9B%BE.png" alt="图">`,
		`<img src="/b/b/a/1/shot.png" alt="say &quot;hi&quot; &amp; 1&lt;2">`,
		// An attachment that isn't shown as an image is linked.
		`<a href="/b/b/a/1/notes.txt" class="image">notes</a>`,
		// Other images stay links, or text where links can't be.
		`<a href="nope.png" class="image">missing</a>`,
		`<a href="https://example.com/x.png" class="image">far</a>`,
		`<a href="//example.com/x.png" class="image">proto</a>`,
		`<p><a href="https://example.com/page"><img src="/b/b/a/1/shot.png" alt="inside"></a> <a href="https://example.com/page">outside</a></p>`)
	lacks(t, r, `<img src="https:`, `<img src="//`, `src="nope.png"`, `<img src="/b/b/a/1/notes.txt"`, "javascript:",
		`class="embed"><img src="/b/b/a/1/shot.png" alt="inside"`)
	// The attachments are still listed.
	expect(t, r, 200, `<img src="/b/b/a/1/shot.png" alt="shot.png"`, `href="/b/b/a/1/notes.txt"`)
}

func TestBodyShowsOtherPostsImagesByAddress(t *testing.T) {
	s := serve(t, post.Identity{})
	ctx := context.Background()
	s.thread(codex, "Files", "see")
	s.attach(codex, 1, "shot.png", "image/png", png)
	s.attach(codex, 1, "notes.txt", "text/plain", []byte("plain"))
	s.reply(claude, 1, "![same thread](/b/b/a/1/shot.png)\n")
	s.board("other", "Other")
	s.threadOn("other", codex, "Elsewhere", "here")
	if _, _, err := s.store.Attach(ctx, codex, "other", 1, "pic.gif", "image/gif", []byte("GIF89a")); err != nil {
		t.Fatal(err)
	}
	s.thread(claude, "Pictures", "![other thread](/b/b/a/1/shot.png)\n\n"+
		"![other board](/b/other/a/1/pic.gif)\n\n"+
		"![escaped](/b/b/a/1/sh%6Ft.png)\n\n"+
		"![text](/b/b/a/1/notes.txt)\n\n"+
		"![query](/b/b/a/1/shot.png?x=1) ![fragment](/b/b/a/1/shot.png#x) ![host](http://127.0.0.1/b/b/a/1/shot.png)\n\n"+
		"![gone](/b/b/a/9/shot.png) ![no board](/b/nope/a/1/shot.png) ![zero](/b/b/a/01/shot.png)\n\n"+
		"![escaped board](/b/%62/a/1/shot.png) ![escaped id](/b/b/a/%31/shot.png) ![](/b/b/a/1/notes.txt)\n")

	expect(t, s.get("/b/b/t/1"), 200, `<a href="/b/b/a/1/shot.png" class="embed"><img src="/b/b/a/1/shot.png" alt="same thread"></a>`)
	r := s.get("/b/b/t/3")
	expect(t, r, 200,
		`<img src="/b/b/a/1/shot.png" alt="other thread">`,
		`<a href="/b/other/a/1/pic.gif" class="embed"><img src="/b/other/a/1/pic.gif" alt="other board"></a>`,
		`<img src="/b/b/a/1/shot.png" alt="escaped">`,
		`<a href="/b/b/a/1/notes.txt" class="image">text</a>`,
		`<a href="/b/b/a/1/shot.png?x=1" class="image">query</a>`,
		`<a href="/b/b/a/1/shot.png#x" class="image">fragment</a>`,
		`<a href="http://127.0.0.1/b/b/a/1/shot.png" class="image">host</a>`,
		`<a href="/b/b/a/9/shot.png" class="image">gone</a>`,
		`<a href="/b/nope/a/1/shot.png" class="image">no board</a>`,
		`<a href="/b/b/a/01/shot.png" class="image">zero</a>`,
		// Only NAME is percent-decoded.
		`<a href="/b/%62/a/1/shot.png" class="image">escaped board</a>`,
		`<a href="/b/b/a/%31/shot.png" class="image">escaped id</a>`,
		// A link without text says image.
		`<a href="/b/b/a/1/notes.txt" class="image">image</a>`)
	lacks(t, r, `<img src="http:`, `alt="query"`, `alt="fragment"`, `alt="gone"`, `alt="text"`, `alt="escaped board"`, `alt="escaped id"`)
}

func TestBodyImagesFollowTheAttachments(t *testing.T) {
	s := serve(t, post.Identity{})
	ctx := context.Background()
	s.thread(codex, "Later", "![later](shot.png)\n")
	r := s.get("/b/b/t/1")
	expect(t, r, 200, `<a href="shot.png" class="image">later</a>`)
	lacks(t, r, `alt="later"`)

	s.attach(codex, 1, "shot.png", "image/png", png)
	expect(t, s.get("/b/b/t/1"), 200, `<img src="/b/b/a/1/shot.png" alt="later">`)

	// Replaced by a file that isn't shown as an image, it is linked.
	s.attach(codex, 1, "shot.png", "text/plain", []byte("plain"))
	r = s.get("/b/b/t/1")
	expect(t, r, 200, `<a href="/b/b/a/1/shot.png" class="image">later</a>`)
	lacks(t, r, `alt="later"`)

	if _, err := s.store.Detach(ctx, codex, "b", 1, "shot.png"); err != nil {
		t.Fatal(err)
	}
	r = s.get("/b/b/t/1")
	expect(t, r, 200, `<a href="shot.png" class="image">later</a>`)
	lacks(t, r, `alt="later"`)
}
