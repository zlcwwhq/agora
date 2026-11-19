package web

import (
	"strconv"
	"testing"
)

func TestSendingOf(t *testing.T) {
	f := form{Nonce: "n1", Title: "A title", Tags: "a b", Body: "A body."}
	s, ok := sendingOf("/new", f)
	if !ok {
		t.Fatal("a form with a nonce isn't repeatable")
	}
	if again, _ := sendingOf("/new", f); again != s {
		t.Error("the same form sent again is another sending")
	}
	changed := []struct {
		name string
		path string
		f    form
	}{
		{"another address", "/t/wsl-1", f},
		{"another nonce", "/new", form{Nonce: "n2", Title: f.Title, Tags: f.Tags, Body: f.Body}},
		{"another title", "/new", form{Nonce: f.Nonce, Title: "A changed title", Tags: f.Tags, Body: f.Body}},
		{"other tags", "/new", form{Nonce: f.Nonce, Title: f.Title, Tags: "a", Body: f.Body}},
		{"another body", "/new", form{Nonce: f.Nonce, Title: f.Title, Tags: f.Tags, Body: "A changed body."}},
		// Fields are delimited, so text can't move from one to the next.
		{"text moved between fields", "/new", form{Nonce: f.Nonce, Title: "A title" + "a", Tags: " b", Body: f.Body}},
	}
	for _, c := range changed {
		if other, _ := sendingOf(c.path, c.f); other == s {
			t.Errorf("%s is the same sending", c.name)
		}
	}
	if _, ok := sendingOf("/new", form{Title: "No nonce"}); ok {
		t.Error("a form without a nonce is repeatable")
	}
}

func TestRememberForgetsTheOldest(t *testing.T) {
	s := &server{sent: map[sending]string{}}
	var first, second sending
	for i := 1; i <= maxSent+1; i++ {
		n := strconv.Itoa(i)
		sent, _ := sendingOf("/new", form{Nonce: n, Title: "Post " + n})
		switch i {
		case 1:
			first = sent
		case 2:
			second = sent
		}
		s.remember(sent, "/t/wsl-"+n+"#wsl-"+n)
	}
	if len(s.sent) != maxSent || len(s.sendings) != maxSent {
		t.Fatalf("remembers %d sendings in a map of %d, want %d", len(s.sendings), len(s.sent), maxSent)
	}
	if _, ok := s.sent[first]; ok {
		t.Error("remembers the oldest sending beyond maxSent")
	}
	if to := s.sent[second]; to != "/t/wsl-2#wsl-2" {
		t.Errorf("the second sending leads to %q", to)
	}
}
