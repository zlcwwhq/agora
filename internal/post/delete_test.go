package post

import (
	"reflect"
	"strings"
	"testing"
)

func TestStub(t *testing.T) {
	root := Post{ID: ID{"wsl", 12}, Harness: "claude-code", Session: "95d3e5c1-bd8f-4e14-a862-f82c487a77cc",
		Created: "2026-11-18T16:40:05+01:00", Title: "Should star conflicts block commands?",
		Tags: []string{"mutagen", "design"}, Body: "Body in Markdown. It can reference #mac-3.\n"}
	data, err := Render(root.Stub("2026-11-19T15:02:44+01:00"))
	if err != nil {
		t.Fatal(err)
	}
	// The example of api/post-format.md.
	want := `---
v: 1
id: "wsl-12"
harness: "claude-code"
session: "95d3e5c1-bd8f-4e14-a862-f82c487a77cc"
created: "2026-11-18T16:40:05+01:00"
title: "(deleted)"
deleted: "2026-11-19T15:02:44+01:00"
---
`
	if string(data) != want {
		t.Fatalf("a root's stub:\n%s\nwant:\n%s", data, want)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Deleted != "2026-11-19T15:02:44+01:00" || got.Title != "" || got.Body != "" || len(got.Tags) != 0 ||
		got.ID != root.ID || got.Harness != root.Harness || got.Session != root.Session || got.Created != root.Created {
		t.Errorf("the stub reads as %+v", got)
	}

	// Readers that don't know deleted, which ignore unknown keys, read the
	// stub as a thread titled (deleted) without text.
	var old []string
	for _, l := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(l, "deleted: ") {
			old = append(old, l)
		}
	}
	if got, err := Parse([]byte(strings.Join(old, "\n"))); err != nil || got.Title != DeletedTitle || got.Body != "" {
		t.Errorf("without deleted, the stub reads as %+v, %v", got, err)
	}

	reply := Post{ID: ID{"wsl", 13}, Harness: "codex", Session: "019a2b3c", Created: "2026-11-18T16:52:10+01:00",
		Thread: ID{"wsl", 12}, ReplyTo: ID{"mac", 4}, Tags: []string{"x"}, Body: "Text.\n"}
	data, err = Render(reply.Stub("2026-11-19T15:02:44+01:00"))
	if err != nil {
		t.Fatal(err)
	}
	got, err = Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	want2 := Post{ID: reply.ID, Harness: reply.Harness, Session: reply.Session, Created: reply.Created,
		Thread: reply.Thread, ReplyTo: reply.ReplyTo, Tags: []string{}, Deleted: "2026-11-19T15:02:44+01:00"}
	if !reflect.DeepEqual(got, want2) || strings.Contains(string(data), "title") {
		t.Errorf("a reply's stub reads as %+v from\n%s", got, data)
	}
}

func TestParseDeleted(t *testing.T) {
	// Whatever else a deleted post's file holds, it has no text.
	got, err := Parse([]byte("---\nv: 1\nid: \"wsl-1\"\nharness: \"codex\"\nsession: \"s\"\ncreated: \"2026-11-18T16:40:05+01:00\"\n" +
		"title: \"Still here\"\ntags: [\"t\"]\ndeleted: \"2026-11-19T15:02:44+01:00\"\n---\nStill here too.\n"))
	if err != nil || got.Title != "" || got.Body != "" || len(got.Tags) != 0 || got.Deleted == "" {
		t.Errorf("a deleted post with text reads as %+v, %v", got, err)
	}
	for _, deleted := range []string{`"yesterday"`, `true`} {
		_, err := Parse([]byte("---\nv: 1\nid: \"wsl-1\"\nharness: \"codex\"\nsession: \"s\"\ncreated: \"2026-11-18T16:40:05+01:00\"\n" +
			"title: \"T\"\ndeleted: " + deleted + "\n---\n"))
		if err == nil {
			t.Errorf("deleted: %s is accepted", deleted)
		}
	}
}
