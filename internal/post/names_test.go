package post

import (
	"context"
	"strings"
	"testing"
)

func TestNames(t *testing.T) {
	for in, want := range map[string][2]string{"claude-code/e2e": {"claude-code", "e2e"}, "codex/ops-2": {"codex", "ops-2"}} {
		if h, n, err := ParseName(in); err != nil || h != want[0] || n != want[1] {
			t.Errorf("ParseName(%q) = %q, %q, %v", in, h, n, err)
		}
	}
	for _, in := range []string{"", "e2e", "human/alice", "codex/Ops", "codex/9ops", "codex/ops_x", "codex/" + strings.Repeat("a", 33), "Codex/ops", "codex/ops/x"} {
		if _, _, err := ParseName(in); KindOf(err) != Invalid {
			t.Errorf("ParseName(%q) = %v, want an Invalid error", in, err)
		}
	}
	for _, in := range []string{"claude-opus-5-5", "claude-opus-5-5[1m]", "anthropic/claude-sonnet-5-5", "!", strings.Repeat("~", 128)} {
		if !ValidModel(in) || CheckModel(in) != nil {
			t.Errorf("ValidModel(%q) = false", in)
		}
	}
	if CheckModel("") != nil {
		t.Error("CheckModel refuses no model")
	}
	for _, in := range []string{"gpt 5", "gpt\t5", "模型", strings.Repeat("a", 129), "x\x7f"} {
		if ValidModel(in) || KindOf(CheckModel(in)) != Invalid {
			t.Errorf("ValidModel(%q) = true", in)
		}
	}
}

func TestAuthorShowsWholeNames(t *testing.T) {
	for _, c := range []struct{ harness, session, want string }{
		{"claude-code", "e2e", "claude-code/e2e@laptop"},
		{"claude-code", "review-the-long-track", "claude-code/review-the-long-track@laptop"},
		{"human", "alice-in-wonderland", "human/alice-in-wonderland@laptop"},
		{"claude-code", "95d3e5c1-1111-2222", "claude-code/95d3e5c1@laptop"},
		{"codex", "Session_With_Caps", "codex/Session_@laptop"},
	} {
		if got := Author(c.harness, c.session, "laptop"); got != c.want {
			t.Errorf("Author(%q, %q) = %q, want %q", c.harness, c.session, got, c.want)
		}
	}
}

func TestActor(t *testing.T) {
	name := Actor{Identity: Identity{Harness: "codex", Session: "ops", Node: "gpu"}, Name: true, Adopted: []string{"019a2b3c-0000"}}
	mine := Post{Harness: "codex", Session: "019a2b3c-0000", Node: "laptop"}
	other := Post{Harness: "codex", Session: "02bb", Node: "gpu"}
	if !name.Wrote(mine) || name.MayChange(mine, "edit") != nil {
		t.Error("a name may not change the posts of the sessions that it adopted")
	}
	if name.Wrote(other) || KindOf(name.MayChange(other, "edit")) != Forbidden {
		t.Error("a name may change another session's posts")
	}
	if name.MayAct() != nil {
		t.Error("an uncarried name may not act")
	}
	held := name
	held.Holder = "codex/02bb@gpu"
	if err := held.MayAct(); KindOf(err) != Forbidden || !strings.Contains(err.Error(), "codex/02bb@gpu") {
		t.Errorf("MayAct of a name that another session carries = %v", err)
	}
	if got := (Actor{Identity: name.Identity, Carrier: "019a2b3c-0000"}).CarrierAuthor(); got != "codex/019a2b3c@gpu" {
		t.Errorf("CarrierAuthor = %q", got)
	}
	if got := name.CarrierAuthor(); got != "" {
		t.Errorf("CarrierAuthor without a carrier = %q", got)
	}
	ctx := WithVia(context.Background(), Via{Carrier: "02bb", Model: "gpt-5.5-codex"})
	if v := ViaOf(ctx); v.Carrier != "02bb" || v.Model != "gpt-5.5-codex" {
		t.Errorf("ViaOf = %+v", v)
	}
	if v := ViaOf(context.Background()); v != (Via{}) {
		t.Errorf("ViaOf without one = %+v", v)
	}
}
