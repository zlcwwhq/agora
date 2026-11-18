package cursor

import (
	"reflect"
	"testing"
)

func TestMarkAbsorbsContiguousNumbers(t *testing.T) {
	c := New()
	for _, n := range []int{1, 2, 4, 5, 7} {
		c.Mark("wsl", n)
	}
	if got, want := *c.Nodes["wsl"], (Range{Contig: 2, Above: []int{4, 5, 7}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if c.Seen("wsl", 3) || c.Seen("wsl", 6) || c.Seen("wsl", 8) || c.Seen("mac", 1) {
		t.Fatal("unmarked numbers count as seen")
	}
	for _, n := range []int{1, 2, 4, 5, 7} {
		if !c.Seen("wsl", n) {
			t.Fatalf("%d isn't seen", n)
		}
	}

	// A late arrival fills the gap, and the cursor catches up past it.
	c.Mark("wsl", 3)
	if got, want := *c.Nodes["wsl"], (Range{Contig: 5, Above: []int{7}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	c.Mark("wsl", 6)
	if got, want := *c.Nodes["wsl"], (Range{Contig: 7}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	c.Mark("wsl", 0)
	c.Mark("wsl", 7)
	if got, want := *c.Nodes["wsl"], (Range{Contig: 7}); !reflect.DeepEqual(got, want) {
		t.Fatalf("marking 0 or a seen number changed the cursor: %+v", got)
	}
}

func TestParseRoundTripsAndNormalizes(t *testing.T) {
	c := New()
	c.Reader = "codex/019a"
	c.Mark("wsl", 1)
	c.Mark("mac", 3)
	got, err := Parse(c.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, c) {
		t.Fatalf("got %+v, want %+v", got, c)
	}
	if string(c.Marshal()) != `{"reader":"codex/019a","nodes":{"mac":{"contig":0,"above":[3]},"wsl":{"contig":1}}}`+"\n" {
		t.Fatalf("marshal: %s", c.Marshal())
	}

	got, err = Parse([]byte(`{"nodes":{"wsl":{"contig":-1,"above":[3,1,2,2,5]},"x":null}}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := (Range{Contig: 3, Above: []int{5}}); !reflect.DeepEqual(*got.Nodes["wsl"], want) || len(got.Nodes) != 1 {
		t.Fatalf("got %+v", got.Nodes)
	}

	if _, err := Parse([]byte("{")); err == nil {
		t.Fatal("parsed invalid JSON")
	}
	if got, err := Parse([]byte("{}")); err != nil || got.Nodes == nil {
		t.Fatalf("empty cursor: %v %v", got, err)
	}
}
