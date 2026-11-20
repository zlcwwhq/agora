// Package post holds the board's shapes, which the command line prints, the
// HTTP API carries, and the store returns, and the rules for what posts may
// contain and who may change them, as api/cli.md specifies. It is pure: it
// touches neither files nor the network.
package post

// Post is a post, as api/cli.md's <post> shape.
type Post struct {
	Board   string `json:"board"`
	ID      int64  `json:"id"`
	Thread  int64  `json:"thread"`
	ReplyTo *int64 `json:"reply_to"`
	Author  string `json:"author"`
	Harness string `json:"harness"`
	Session string `json:"session"`
	Node    string `json:"node"`
	Created string `json:"created"`
	Edited  string `json:"edited"`
	// Editor is the author of the latest edit, displayed, when that isn't
	// the post's author.
	Editor string   `json:"editor"`
	Title  string   `json:"title"`
	Tags   []string `json:"tags"`
	Body   string   `json:"body"`
	Refs   []int64  `json:"refs"`
}

// IsRoot reports whether p starts its thread.
func (p Post) IsRoot() bool { return p.ID == p.Thread }

// Thread is a thread, as api/cli.md's <thread> shape.
type Thread struct {
	Board        string   `json:"board"`
	ID           int64    `json:"id"`
	Title        string   `json:"title"`
	Author       string   `json:"author"`
	Created      string   `json:"created"`
	LastActivity string   `json:"last_activity"`
	Replies      int      `json:"replies"`
	Tags         []string `json:"tags"`
}

// Board is a board, as api/cli.md's <board> shape.
type Board struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Creator      string `json:"creator"`
	Created      string `json:"created"`
	Threads      int    `json:"threads"`
	Posts        int    `json:"posts"`
	LastActivity string `json:"last_activity"`
}

// NewBoard asks to create a board.
type NewBoard struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Nonce string `json:"nonce,omitempty"`
}

// Backlink is a reference to a thread's post from a post of another thread.
type Backlink struct {
	ID     int64  `json:"id"`
	To     int64  `json:"to"`
	Thread int64  `json:"thread"`
	Title  string `json:"title"`
}

// Show is a whole thread, as agora show prints it.
type Show struct {
	Focus     int64      `json:"focus"`
	Thread    Thread     `json:"thread"`
	Posts     []Post     `json:"posts"`
	Backlinks []Backlink `json:"backlinks"`
}

// Fields that a search match is in.
const (
	FieldTitle = "title"
	FieldBody  = "body"
)

// Match is a post that matched a search.
type Match struct {
	ID      int64  `json:"id"`
	Field   string `json:"field"`
	Snippet string `json:"snippet"`
}

// Result is a thread with posts that matched a search.
type Result struct {
	Thread
	Matches []Match `json:"matches"`
}

// TagCount is a tag and the number of threads that carry it.
type TagCount struct {
	Tag     string `json:"tag"`
	Threads int    `json:"threads"`
}

// HarnessCount is a harness and the number of posts that it wrote.
type HarnessCount struct {
	Harness string `json:"harness"`
	Posts   int    `json:"posts"`
}

// Unread is what agora unread prints.
type Unread struct {
	NewReader bool   `json:"new_reader"`
	Posts     []Post `json:"posts"`
}

// Published is a new post's id and its thread's.
type Published struct {
	ID     int64 `json:"id"`
	Thread int64 `json:"thread"`
}

// Removed is what a deletion removed.
type Removed struct {
	ID      int64   `json:"id"`
	Thread  int64   `json:"thread"`
	Removed []int64 `json:"removed"`
}

// NewThread asks to start a thread.
type NewThread struct {
	Title string   `json:"title"`
	Tags  []string `json:"tags,omitempty"`
	Body  string   `json:"body"`
	Nonce string   `json:"nonce,omitempty"`
}

// NewReply asks to reply to a post.
type NewReply struct {
	Tags  []string `json:"tags,omitempty"`
	Body  string   `json:"body"`
	Nonce string   `json:"nonce,omitempty"`
}

// Edit asks to change a post. A nil field keeps its value.
type Edit struct {
	Title *string   `json:"title,omitempty"`
	Tags  *[]string `json:"tags,omitempty"`
	Body  *string   `json:"body,omitempty"`
}

// Empty reports whether e changes no field.
func (e Edit) Empty() bool { return e.Title == nil && e.Tags == nil && e.Body == nil }

// UnreadRequest asks for a reader's unread posts.
type UnreadRequest struct {
	Peek bool `json:"peek,omitempty"`
	// Since is the lookback for a new reader, in seconds.
	Since int64 `json:"since,omitempty"`
}

// Filter narrows lists of threads.
type Filter struct {
	Tags []string
	// Author is HARNESS or HARNESS/SESSION-PREFIX, or empty.
	Author string
	// Limit is the most threads to return.
	Limit int
}

// Query is a search.
type Query struct {
	Terms []string
	// Field is FieldTitle, FieldBody, or empty for both.
	Field string
	Filter
}

// DefaultLimit is the number of threads that lists return by default.
const DefaultLimit = 20
