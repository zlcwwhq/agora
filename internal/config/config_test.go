package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(dir, home string, vars map[string]string) Env {
	return Env{
		Dir: dir, Home: home,
		Getenv:   func(k string) string { return vars[k] },
		Hostname: func() (string, error) { return "DESKTOP-Ab12.example.com", nil },
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultsWithoutFile(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := Load("", "", env(filepath.Join(home, "a"), home, nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != "" || c.URL() != "http://127.0.0.1:7777" || c.Node != "desktop-ab12" || c.Author != "" {
		t.Errorf("config = %+v", c)
	}
}

func TestFindNearestThenHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	proj := filepath.Join(home, "proj")
	write(t, filepath.Join(home, FileName), "[server]\nport = 8000\n")
	write(t, filepath.Join(proj, FileName), "[server]\nhost = \"10.0.0.2\"\n[whoami]\nnode = \"laptop\"\nauthor = \"human/alice\"\n")
	deep := filepath.Join(proj, "a", "b")

	c, err := Load("", "", env(deep, home, nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != filepath.Join(proj, FileName) || c.Addr() != "10.0.0.2:7777" || c.Node != "laptop" || c.Author != "human/alice" {
		t.Errorf("project config = %+v", c)
	}
	c, err = Load("", "", env(filepath.Join(home, "elsewhere"), home, nil))
	if err != nil || c.Path != filepath.Join(home, FileName) || c.Port != 8000 {
		t.Errorf("home config = %+v, %v", c, err)
	}
}

func TestSearchStopsAtHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	write(t, filepath.Join(root, FileName), "[server]\nport = 8000\n")
	if err := os.MkdirAll(filepath.Join(home, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := Load("", "", env(filepath.Join(home, "proj"), home, nil))
	if err != nil || c.Path != "" || c.Port != DefaultPort {
		t.Errorf("a file above home was used: %+v, %v", c, err)
	}
}

func TestOverrides(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "custom.toml")
	write(t, path, "[server]\nhost = \"example.org\"\nport = 9000\n[whoami]\nnode = \"filenode\"\n")
	vars := map[string]string{"AGORA_CONFIG": path, "AGORA_SERVER": "[::1]:7000", "AGORA_NODE": "envnode"}
	c, err := Load("", "", env(root, root, vars))
	if err != nil || c.Path != path || c.Addr() != "[::1]:7000" || c.Node != "envnode" {
		t.Errorf("env overrides = %+v, %v", c, err)
	}
	c, err = Load("", "localhost:7100", env(root, root, vars))
	if err != nil || c.Addr() != "localhost:7100" {
		t.Errorf("--server = %+v, %v", c, err)
	}
	if _, err := Load(filepath.Join(root, "missing"), "", env(root, root, nil)); err == nil {
		t.Error("a missing --config file was accepted")
	}
	for _, bad := range []map[string]string{{"AGORA_SERVER": "nope"}, {"AGORA_SERVER": "h:0"}, {"AGORA_NODE": "Bad"}} {
		if _, err := Load("", "", env(root, root, bad)); err == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
}

func TestOldBoardIsAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "home", FileName, "posts"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Load("", "", env(filepath.Join(root, "home", "x"), filepath.Join(root, "home"), nil))
	if err == nil || !strings.Contains(err.Error(), "agora 1") {
		t.Errorf("Load with an agora 1 board: %v", err)
	}
}

func TestInvalidFiles(t *testing.T) {
	for _, content := range []string{
		"[server]\nhots = \"x\"\n",
		"[sever]\nhost = \"x\"\n",
		"[server]\nport = \"7777\"\n",
		"[server]\nport = 70000\n",
		"[server]\nhost = \"http://x\"\n",
		"[server]\nhost = \"x:80\"\n",
		"[whoami]\nnode = \"Laptop\"\n",
		"[whoami]\nauthor = \"alice\"\n",
		"not toml",
	} {
		path := filepath.Join(t.TempDir(), FileName)
		write(t, path, content)
		if _, err := Read(path); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("Read(%q) = %v, want an error naming the file", content, err)
		}
	}
}

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := Write(path, "127.0.0.1", 7777, "laptop", "human/alice", false); err != nil {
		t.Fatal(err)
	}
	f, err := Read(path)
	if err != nil || f.Server.Host != "127.0.0.1" || f.Server.Port != 7777 || f.Whoami.Node != "laptop" || f.Whoami.Author != "human/alice" {
		t.Errorf("written file reads as %+v, %v", f, err)
	}
	if err := Write(path, "127.0.0.1", 7777, "", "", false); err == nil {
		t.Error("Write replaced a file without force")
	}
	if err := Write(path, "::1", 8000, "", "", true); err != nil {
		t.Errorf("Write with force: %v", err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "whoami") {
		t.Errorf("file without node or author has [whoami]:\n%s", data)
	}
	if err := Write(dir, "127.0.0.1", 7777, "", "", true); err == nil {
		t.Error("Write replaced a directory")
	}
}

func TestNodeFromHostname(t *testing.T) {
	for in, want := range map[string]string{
		"DESKTOP-AB12": "desktop-ab12", "gpu.lan": "gpu", "1box": "host", "": "host",
		"my_host": "myhost", strings.Repeat("a", 40): strings.Repeat("a", 32),
	} {
		if got := NodeFromHostname(in); got != want {
			t.Errorf("NodeFromHostname(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStateDir(t *testing.T) {
	get := func(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }
	abs := t.TempDir()
	if d, _ := StateDir(get(map[string]string{"XDG_STATE_HOME": abs}), "linux", "/home/me"); d != filepath.Join(abs, "agora") {
		t.Errorf("XDG: %s", d)
	}
	if d, _ := StateDir(get(map[string]string{"XDG_STATE_HOME": "rel"}), "darwin", "/home/me"); d != filepath.Join("/home/me", ".local", "state", "agora") {
		t.Errorf("relative XDG: %s", d)
	}
	if d, _ := StateDir(get(map[string]string{"LOCALAPPDATA": `C:\L`}), "windows", ""); d != filepath.Join(`C:\L`, "agora") {
		t.Errorf("windows: %s", d)
	}
}
