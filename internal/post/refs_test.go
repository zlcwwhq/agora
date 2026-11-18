package post

import (
	"reflect"
	"testing"
)

func TestRefs(t *testing.T) {
	self := ID{Node: "wsl", N: 1}
	ids := func(ss ...string) []ID {
		out := []ID{}
		for _, s := range ss {
			id, err := ParseID(s)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, id)
		}
		return out
	}
	for _, c := range []struct {
		body string
		want []ID
	}{
		{"see #mac-3 and #gpu-12.", ids("mac-3", "gpu-12")},
		{"#mac-3 at the start", ids("mac-3")},
		{"见#wsl-12的讨论，还有（#mac-2）", ids("wsl-12", "mac-2")},
		{"duplicates #mac-3 #mac-3, self #wsl-1", ids("mac-3")},
		{"page#sec-2 &#x-1 a/#b-1 ##c-1 .#d-1 _#e-1 -#f-1", ids()},
		{"#mac-3x #mac-3_ #mac-3- #mac-03 #Mac-3 #mac-", ids()},
		{"#abcdefghijklmnopq-1 is too long, #abcdefghijklmnop-1 isn't", ids("abcdefghijklmnop-1")},
		{"`#mac-1` and ``a ` #mac-2`` but #mac-3 and `unclosed #mac-4", ids("mac-3", "mac-4")},
		{"```\n#mac-1\n```\n#mac-2\n~~~~go\n#mac-3\n~~~\n#mac-4\n~~~~\n#mac-5", ids("mac-2", "mac-5")},
		{"   ```\n#mac-1\n   ```\n#mac-2", ids("mac-2")},
		{"    ```\n#mac-1", ids("mac-1")},
		{"```\n#mac-1", ids()},
		{"#mac-3\n#gpu-1(", ids("mac-3", "gpu-1")},
	} {
		if got := Refs(c.body, self); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Refs(%q) = %v, want %v", c.body, got, c.want)
		}
	}
}
