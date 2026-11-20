package server

import (
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/zlcwwhq/agora/internal/post"
)

// mentionsKey holds the map[string]bool of the mentions of a body, as the
// server stored them: only those become links.
var mentionsKey = parser.NewContextKey()

// kindMention is the kind of mentionNode.
var kindMention = ast.NewNodeKind("Mention")

// mentionNode is a mention, @HARNESS or @HARNESS/SESSION, a node of its own
// for the reason that refNode is.
type mentionNode struct {
	ast.BaseInline
	// Mention is as written, without its @.
	Mention string
	// Board is the board of the body, whose threads by the author the
	// mention links to.
	Board string
}

func (n *mentionNode) Kind() ast.NodeKind { return kindMention }

func (n *mentionNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Mention": n.Mention}, nil)
}

// mentionParser finds mentions with the rules of internal/post, and keeps
// those that the server stored.
type mentionParser struct{}

func (mentionParser) Trigger() []byte { return []byte{'@'} }

func (mentionParser) Parse(_ ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	var prev byte
	// A line's start and characters outside ASCII both allow a mention.
	if r := block.PrecendingCharacter(); r != '\n' && r < utf8.RuneSelf {
		prev = byte(r)
	}
	m, n, ok := post.MentionAt(prev, string(line))
	if !ok {
		return nil
	}
	if stored, _ := pc.Get(mentionsKey).(map[string]bool); !stored[m.String()] {
		return nil
	}
	board, _ := pc.Get(boardKey).(string)
	if !post.ValidBoard(board) {
		return nil
	}
	block.Advance(n)
	return &mentionNode{Mention: m.String(), Board: board}
}

// mentionRenderer renders mentions as links to the threads that the authors
// they mention started, or as text within links.
type mentionRenderer struct{}

func (mentionRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMention, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		n := node.(*mentionNode)
		// Harnesses and sessions are ASCII letters, digits, '.', '_', and
		// '-', and boards' ids lowercase letters, digits, and '-', which
		// need no escaping, in an address's query or in text.
		if inLink(n) {
			_, _ = w.WriteString("@" + n.Mention)
		} else {
			_, _ = w.WriteString(`<a href="` + boardURL(n.Board, "") + `?author=` + n.Mention + `" class="mention">@` + n.Mention + `</a>`)
		}
		return ast.WalkContinue, nil
	})
}
