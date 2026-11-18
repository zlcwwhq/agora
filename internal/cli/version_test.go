package cli

import (
	"bytes"
	"testing"
)

func TestVersion(t *testing.T) {
	for _, c := range []struct {
		version string
		args    []string
		out     string
		code    int
	}{
		{"v0.1.1", []string{"--version"}, "agora v0.1.1\n", 0},
		{"v0.1.2-0.20261118204955-ccd196632617+dirty", []string{"--version"}, "agora v0.1.2-0.20261118204955-ccd196632617+dirty\n", 0},
		{"(devel)", []string{"--version"}, "agora (devel)\n", 0},
		{"", []string{"--version"}, "agora (devel)\n", 0},
		{"v0.1.1", []string{"--version", "list"}, "", 2},
		{"v0.1.1", []string{"list", "--version"}, "", 2},
	} {
		var out, errOut bytes.Buffer
		code := Run(Env{
			Args: c.args, Getenv: func(string) string { return "" }, GOOS: "linux",
			Stdin: unreadable{t}, Stdout: &out, Stderr: &errOut, Version: c.version,
		})
		if code != c.code || out.String() != c.out {
			t.Errorf("agora %v with version %q: status %d, output %q, want %d and %q\n%s",
				c.args, c.version, code, out.String(), c.code, c.out, errOut.String())
		}
	}
}
