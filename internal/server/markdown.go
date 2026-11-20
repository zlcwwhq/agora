package server

import (
	"bytes"
	"html/template"
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
// autolinks, and task lists, references as links, and without raw HTML, which
// goldmark leaves out unless it is told to be unsafe.
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
		parser.WithInlineParsers(util.Prioritized(refParser{}, 1000)),
		parser.WithASTTransformers(util.Prioritized(safeLinks{}, 1000)),
	),
	goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(refRenderer{}, 1000))),
)

// refsKey holds the map[int64]bool of the posts that a body refers to, as
// the server stored them: only those #N become links.
var refsKey = parser.NewContextKey()

// boardKey holds the id of the board of the posts that a body refers to.
var boardKey = parser.NewContextKey()

// render renders body, a post's on board, as HTML, linking the references
// in refs.
func render(body, board string, refs []int64) (template.HTML, error) {
	set := make(map[int64]bool, len(refs))
	for _, id := range refs {
		set[id] = true
	}
	pc := parser.NewContext()
	pc.Set(refsKey, set)
	pc.Set(boardKey, board)
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(body), &buf, parser.WithContext(pc)); err != nil {
		return "", err
	}
	// goldmark escapes text and leaves out raw HTML.
	return template.HTML(buf.String()), nil
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
// interface, turns other links into their text, shows images as links to them,
// since pages load nothing from other origins, and keeps links out of links.
type safeLinks struct{}

func (safeLinks) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
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
			if inLink(n) || !allowedURL(n.Destination) {
				unwrap(n)
				continue
			}
			link := ast.NewLink()
			link.Destination, link.Title = n.Destination, n.Title
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
