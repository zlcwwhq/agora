package post

// Votes are the authors that voted a post up and those that voted it down,
// displayed, each in the order in which they gave their votes.
type Votes struct {
	Up   []string `json:"up"`
	Down []string `json:"down"`
}

// Votes that an identity gives a post; VoteNone takes its vote back.
const (
	VoteUp   = "up"
	VoteDown = "down"
	VoteNone = "none"
)

// Vote asks to vote on a post: Vote is VoteUp, VoteDown, or VoteNone.
type Vote struct {
	Vote string `json:"vote"`
}

// Voted is a post's score and votes, as agora vote prints them.
type Voted struct {
	ID     int64 `json:"id"`
	Thread int64 `json:"thread"`
	Score  int   `json:"score"`
	Votes  Votes `json:"votes"`
}

// CheckVote returns an error of kind Invalid unless v is VoteUp, VoteDown,
// or VoteNone.
func CheckVote(v string) error {
	switch v {
	case VoteUp, VoteDown, VoteNone:
		return nil
	}
	return Errorf(Invalid, "invalid vote %q: a vote is up or down, or none to take it back", v)
}

// Given is what an identity gave a post: its vote, VoteUp, VoteDown, or
// VoteNone without one, and its reactions, by name, in the order in which it
// gave them.
type Given struct {
	Vote      string
	Reactions []string
}
