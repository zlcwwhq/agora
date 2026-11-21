package server

import (
	"bytes"
	"html/template"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/zlcwwhq/agora/internal/post"
)

// markdown renders bodies: CommonMark with GitHub's tables, strikethrough,
// autolinks, and task lists, references and mentions as links, images only
// from attachments, and without raw HTML, which goldmark leaves out unless
// it is told to be unsafe.
var markdown = goldmark.New(
	// GitHub's extensions, with tables aligning cells by attribute: pages
	// allow no style attributes.
	goldmark.WithExtensions(
		extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)),
		extension.Strikethrough, extension.Linkify, extension.TaskList,
	),
	goldmark.WithParserOptions(
		// Lower numbers run first; the default inline parsers don't
		// trigger on '#'.
		parser.WithInlineParsers(util.Prioritized(refParser{}, 1000), util.Prioritized(mentionParser{}, 1000)),
		parser.WithASTTransformers(util.Prioritized(safeLinks{}, 1000)),
	),
	goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(refRenderer{}, 1000), util.Prioritized(mentionRenderer{}, 1000))),
)

// refsKey holds the map[int64]bool of the posts that a body refers to, as
// the server stored them: only those #N become links.
var refsKey = parser.NewContextKey()

// boardKey holds the id of the board of the posts that a body refers to.
var boardKey = parser.NewContextKey()

// imagesKey holds the *images of the body.
var imagesKey = parser.NewContextKey()

// render renders the body of p, a post on board, as HTML, linking the
// references and the mentions that the server stored, and showing the
// images that name attachments: p's own, or those that find finds.
func render(p post.Post, board string, find findAttachment) (template.HTML, error) {
	set := make(map[int64]bool, len(p.Refs))
	for _, id := range p.Refs {
		set[id] = true
	}
	mentioned := make(map[string]bool, len(p.Mentions))
	for _, m := range p.Mentions {
		mentioned[m] = true
	}
	im := &images{board: board, id: p.ID, own: p.Attachments, find: find}
	pc := parser.NewContext()
	pc.Set(refsKey, set)
	pc.Set(mentionsKey, mentioned)
	pc.Set(boardKey, board)
	pc.Set(imagesKey, im)
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(p.Body), &buf, parser.WithContext(pc)); err != nil {
		return "", err
	}
	if im.err != nil {
		return "", im.err
	}
	// goldmark escapes text and leaves out raw HTML.
	return template.HTML(buf.String()), nil
}

// findAttachment finds post id's attachment name on board, and reports
// whether it exists.
type findAttachment func(board string, id int64, name string) (post.Attachment, bool, error)

// images finds the attachments that a body's images name: the post's own,
// by name, and any post's, by address.
type images struct {
	// board and id are the post's, and own its attachments.
	board string
	id    int64
	own   []post.Attachment
	// find finds other attachments; nil finds none.
	find findAttachment
	// err is find's first failure, after which it isn't asked again.
	err error
}

// attachment returns the attachment that an image's destination names, as
// goldmark parsed it, and its address on the pages, if it exists.
func (im *images) attachment(destination []byte) (post.Attachment, string, bool) {
	if im == nil {
		return post.Attachment{}, "", false
	}
	// Resolve escapes and references as goldmark does when it renders a
	// destination.
	dest := string(util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(destination))))
	names := []string{dest}
	if unescaped, err := url.PathUnescape(dest); err == nil && unescaped != dest {
		names = append(names, unescaped)
	}
	for _, name := range names {
		for _, a := range im.own {
			if a.Name == name {
				return a, attachmentURL(im.board, im.id, name), true
			}
		}
	}
	board, id, name, ok := attachmentPath(dest)
	if !ok || im.find == nil || im.err != nil {
		return post.Attachment{}, "", false
	}
	a, found, err := im.find(board, id, name)
	if err != nil {
		im.err = err
		return post.Attachment{}, "", false
	}
	if !found {
		return post.Attachment{}, "", false
	}
	return a, attachmentURL(board, id, name), true
}

// attachmentPath parses an attachment's address on the pages,
// /b/BOARD/a/ID/NAME, without a scheme, host, query, or fragment.
func attachmentPath(dest string) (board string, id int64, name string, ok bool) {
	rest, found := strings.CutPrefix(dest, "/b/")
	if !found || strings.ContainsAny(rest, "?#") {
		return "", 0, "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 4 || parts[1] != "a" || !post.ValidBoard(parts[0]) {
		return "", 0, "", false
	}
	id, err := post.ParseID(parts[2])
	if err != nil {
		return "", 0, "", false
	}
	name, err = url.PathUnescape(parts[3])
	if err != nil || post.CheckAttachmentName(name) != nil {
		return "", 0, "", false
	}
	return parts[0], id, name, true
}

// kindRef is the kind of refNode.
var kindRef = ast.NewNodeKind("Ref")

// refNode is a reference, #<id>. It is a node of its own rather than a link,
// since links may not contain links: a reference in a link's text would keep
// the link from being one.
type refNode struct {
	ast.BaseInline
	ID int64
	// Board is the board of the post referred to, and of the body.
	Board string
}

func (n *refNode) Kind() ast.NodeKind { return kindRef }

func (n *refNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"ID": strconv.FormatInt(n.ID, 10)}, nil)
}

// refParser finds references with the rules of internal/post, and keeps those
// that the server stored. goldmark parses code spans and blocks itself, so
// references in code stay text, as the format says.
type refParser struct{}

func (refParser) Trigger() []byte { return []byte{'#'} }

func (refParser) Parse(_ ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	var prev byte
	// A line's start and characters outside ASCII both allow a reference.
	if r := block.PrecendingCharacter(); r != '\n' && r < utf8.RuneSelf {
		prev = byte(r)
	}
	id, n, ok := post.RefAt(prev, string(line))
	if !ok {
		return nil
	}
	if refs, _ := pc.Get(refsKey).(map[int64]bool); !refs[id] {
		return nil
	}
	board, _ := pc.Get(boardKey).(string)
	if !post.ValidBoard(board) {
		return nil
	}
	block.Advance(n)
	return &refNode{ID: id, Board: board}
}

// refRenderer renders references as links to their posts, or as text within
// links.
type refRenderer struct{}

func (refRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindRef, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		n := node.(*refNode)
		// Ids are digits, and boards' ids lowercase letters, digits, and
		// '-', which need no escaping.
		id := strconv.FormatInt(n.ID, 10)
		if inLink(n) {
			_, _ = w.WriteString("#" + id)
		} else {
			_, _ = w.WriteString(`<a href="` + inThread(n.Board, n.ID, n.ID) + `" class="ref">#` + id + `</a>`)
		}
		return ast.WalkContinue, nil
	})
}

// safeLinks keeps links to http, https, and mailto addresses and within the
// interface, turns other links into their text, shows images only from
// attachments, since pages load nothing from other origins, and others as
// links to them, and keeps links out of links.
type safeLinks struct{}

func (safeLinks) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	source := reader.Source()
	im, _ := pc.Get(imagesKey).(*images)
	var nodes []ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n.(type) {
		case *ast.Link, *ast.AutoLink, *ast.Image:
			if entering {
				nodes = append(nodes, n)
			}
		}
		return ast.WalkContinue, nil
	})
	// nodes are in document order, so outer links are settled before the
	// links inside them.
	for _, n := range nodes {
		switch n := n.(type) {
		case *ast.Link:
			if inLink(n) || !allowedURL(n.Destination) {
				unwrap(n)
			}
		case *ast.AutoLink:
			if inLink(n) || (n.AutoLinkType == ast.AutoLinkURL && !allowedURL(n.URL(source))) {
				n.Parent().ReplaceChild(n.Parent(), n, ast.NewString(n.Label(source)))
			}
		case *ast.Image:
			destination := n.Destination
			a, src, ok := im.attachment(n.Destination)
			switch {
			case ok && showsImage(a.Type):
				// Shown from the attachment's address, which links to it
				// unless the image is in a link.
				n.Destination = []byte(src)
				if !inLink(n) {
					link := ast.NewLink()
					link.Destination = n.Destination
					link.SetAttributeString("class", []byte("embed"))
					n.Parent().ReplaceChild(n.Parent(), n, link)
					link.AppendChild(link, n)
				}
				continue
			case ok:
				destination = []byte(src)
			case !allowedURL(n.Destination):
				unwrap(n)
				continue
			}
			if inLink(n) {
				unwrap(n)
				continue
			}
			link := ast.NewLink()
			link.Destination, link.Title = destination, n.Title
			link.SetAttributeString("class", []byte("image"))
			for c := n.FirstChild(); c != nil; c = n.FirstChild() {
				link.AppendChild(link, c)
			}
			if !link.HasChildren() {
				link.AppendChild(link, ast.NewString([]byte("image")))
			}
			n.Parent().ReplaceChild(n.Parent(), n, link)
		}
	}
}

// inLink reports whether n is inside a link.
func inLink(n ast.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.(type) {
		case *ast.Link, *ast.AutoLink:
			return true
		}
	}
	return false
}

// unwrap replaces n by its children.
func unwrap(n ast.Node) {
	parent := n.Parent()
	for c := n.FirstChild(); c != nil; c = n.FirstChild() {
		parent.InsertBefore(parent, n, c)
	}
	parent.RemoveChild(parent, n)
}

// allowedURL reports whether a link may lead to url: one without a scheme,
// which stays within the interface, or one whose scheme is http, https, or
// mailto.
func allowedURL(url []byte) bool {
	s := string(url)
	i := strings.IndexAny(s, ":/?#")
	if i < 0 || s[i] != ':' {
		return true
	}
	switch strings.ToLower(s[:i]) {
	case "http", "https", "mailto":
		return true
	}
	return false
}
