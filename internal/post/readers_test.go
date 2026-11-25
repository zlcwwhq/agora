package post

import (
	"slices"
	"strings"
	"testing"
)

func mentionStrings(ms []Mention) []string {
	out := []string{}
	for _, m := range ms {
		out = append(out, m.String())
	}
	return out
}

func TestMentions(t *testing.T) {
	body := "ask @codex. and @claude-code/95d3e5c1, see #12\n" +
		"@human/alice: 见@antigravity的 (@codex/019a.) @codex again\n" +
		"alice@laptop a/@x b-@y c_@z d.@w @@v @Codex @codeX @codex/ @codex/... @codex/019a/x @q_ @codex/a_ok-\n" +
		"@" + strings.Repeat("a", 33) + " @" + strings.Repeat("b", 32) + " @s/" + strings.Repeat("c", 129) + "\n" +
		"`@inline` and ``@two ` @spans`` too\n" +
		"```\n@fenced\n```\n" +
		"~~~~\n@tilde\n~~~\n@still\n~~~~\n" +
		"@last/a.b_c-d"
	want := []string{
		"codex", "claude-code/95d3e5c1", "human/alice", "antigravity", "codex/019a", "codex/a_ok-",
		strings.Repeat("b", 32), "last/a.b_c-d",
	}
	if got := mentionStrings(Mentions(body)); !slices.Equal(got, want) {
		t.Fatalf("Mentions =\n%v\nwant\n%v", got, want)
	}
	if got := Mentions("no mentions"); got == nil || len(got) != 0 {
		t.Errorf("Mentions without any = %#v, want an empty slice", got)
	}
	if m, n, ok := MentionAt(' ', "@codex/019a.. end"); !ok || m != (Mention{"codex", "019a"}) || n != 11 {
		t.Errorf("MentionAt = %+v, %d, %v", m, n, ok)
	}
}

func TestMentionNames(t *testing.T) {
	for _, c := range []struct {
		m                Mention
		harness, session string
		want             bool
	}{
		{Mention{"codex", ""}, "codex", "019a2b3c", true},
		{Mention{"codex", "019a"}, "codex", "019a2b3c", true},
		{Mention{"codex", "019a2b3c"}, "codex", "019a2b3c", true},
		{Mention{"codex", "019b"}, "codex", "019a2b3c", false},
		{Mention{"codex", "019a2b3c-more"}, "codex", "019a2b3c", false},
		{Mention{"claude-code", ""}, "codex", "019a2b3c", false},
	} {
		if got := c.m.Names(c.harness, c.session); got != c.want {
			t.Errorf("%v.Names(%s/%s) = %v, want %v", c.m, c.harness, c.session, got, c.want)
		}
	}
}

func TestParseReaction(t *testing.T) {
	for in, want := range map[string]string{
		"+1": "+1", "\U0001F44D": "+1", "-1": "-1", "\U0001F44E": "-1", "laugh": "laugh", "\U0001F604": "laugh",
		"hooray": "hooray", "\U0001F389": "hooray", "confused": "confused", "\U0001F615": "confused",
		"heart": "heart", "\u2764\uFE0F": "heart", "\u2764": "heart", "rocket": "rocket", "\U0001F680": "rocket",
		"eyes": "eyes", "\U0001F440": "eyes", "ack": "eyes",
	} {
		if got, err := ParseReaction(in); err != nil || got != want {
			t.Errorf("ParseReaction(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "Eyes", " eyes", ":+1:", "thumbsup", "\u2714\uFE0F", "\U0001F44D\U0001F3FD", "已读", "ack "} {
		if got, err := ParseReaction(bad); KindOf(err) != Invalid || !strings.Contains(err.Error(), "+1 \U0001F44D, -1 \U0001F44E") {
			t.Errorf("ParseReaction(%q) = %q, %v, want an error of kind Invalid that lists the reactions", bad, got, err)
		}
	}
	if len(GitHubReactions) != 8 {
		t.Fatalf("%d reactions, want GitHub's 8", len(GitHubReactions))
	}
	for _, r := range GitHubReactions {
		if Emoji(r.Name) != r.Emoji {
			t.Errorf("Emoji(%q) = %q, want %q", r.Name, Emoji(r.Name), r.Emoji)
		}
	}
	if Emoji("unknown") != "unknown" {
		t.Errorf("Emoji of an unknown name = %q", Emoji("unknown"))
	}
}

func TestChangeTags(t *testing.T) {
	for _, c := range []struct {
		tags, add, remove, want []string
	}{
		{[]string{"a", "b", "c"}, []string{"D", "a", "e", "d"}, []string{"b", "nope"}, []string{"a", "c", "d", "e"}},
		{[]string{"a", "b"}, nil, []string{"A"}, []string{"b"}},
		{[]string{"a", "b"}, []string{"a"}, []string{"a"}, []string{"b", "a"}},
		{nil, nil, nil, []string{}},
	} {
		got, err := ChangeTags(c.tags, c.add, c.remove)
		if err != nil || !slices.Equal(got, c.want) || got == nil {
			t.Errorf("ChangeTags(%v, +%v, -%v) = %v, %v; want %v", c.tags, c.add, c.remove, got, err, c.want)
		}
	}
	if _, err := ChangeTags(nil, []string{"a b"}, nil); KindOf(err) != Invalid {
		t.Errorf("adding an invalid tag: %v", err)
	}
	if _, err := ChangeTags(nil, nil, []string{"#x"}); KindOf(err) != Invalid {
		t.Errorf("removing an invalid tag: %v", err)
	}
}

func TestEditCheck(t *testing.T) {
	tags := []string{"x"}
	if err := (Edit{AddTags: &tags}).Check(); err != nil {
		t.Errorf("add_tags alone: %v", err)
	}
	if err := (Edit{RemoveTags: &tags}).Check(); err != nil {
		t.Errorf("remove_tags alone: %v", err)
	}
	for _, e := range []Edit{{}, {Tags: &tags, AddTags: &tags}, {Tags: &tags, RemoveTags: &tags}} {
		if err := e.Check(); KindOf(err) != Invalid {
			t.Errorf("Check(%+v) = %v, want Invalid", e, err)
		}
	}
	got, err := Edit{AddTags: &tags}.NewTags([]string{"a"})
	if err != nil || !slices.Equal(got, []string{"a", "x"}) {
		t.Errorf("NewTags = %v, %v", got, err)
	}
}
