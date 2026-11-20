// Package config finds and reads the configuration file, .agora, that names
// the server, the board, and who is using them, as api/config.md specifies.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"

	"github.com/zlcwwhq/agora/internal/post"
)

// FileName is the configuration file's name, which init writes in the
// working directory and commands search for.
const FileName = ".agora"

// The defaults of [server].
const (
	DefaultHost = "127.0.0.1"
	DefaultPort = 7777
)

// Env is what finding the configuration depends on.
type Env struct {
	// Dir is the working directory, or "" if it is unknown.
	Dir string
	// Home is the home directory, or "" if it is unknown.
	Home   string
	Getenv func(string) string
	// Hostname returns the machine's host name.
	Hostname func() (string, error)
}

// File is the content of a configuration file.
type File struct {
	Server struct {
		Host string `toml:"host"`
		Port int    `toml:"port"`
	} `toml:"server"`
	Board struct {
		ID string `toml:"id"`
	} `toml:"board"`
	Whoami struct {
		Node   string `toml:"node"`
		Author string `toml:"author"`
	} `toml:"whoami"`
}

// Config is the configuration that commands use, with its overrides
// applied.
type Config struct {
	// Path is the file that was found, or "" if none was.
	Path string
	Host string
	Port int
	// Board is the board's id, or "" if none is named.
	Board string
	// Node is this machine's node.
	Node string
	// Author is [whoami] author, HARNESS/SESSION, or "".
	Author string
}

// Addr returns the server's address, HOST:PORT.
func (c Config) Addr() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }

// URL returns the server's base URL.
func (c Config) URL() string { return "http://" + c.Addr() }

// Load returns the configuration: the file named by flag, else the one that
// Find finds, with the overrides of server and board, the --server and
// --board flags, and of the environment.
func Load(flag, server, board string, env Env) (Config, error) {
	path, err := Find(flag, env)
	if err != nil {
		return Config{}, err
	}
	c := Config{Path: path, Host: DefaultHost, Port: DefaultPort}
	if path != "" {
		f, err := Read(path)
		if err != nil {
			return Config{}, err
		}
		if f.Server.Host != "" {
			c.Host = f.Server.Host
		}
		if f.Server.Port != 0 {
			c.Port = f.Server.Port
		}
		c.Board, c.Node, c.Author = f.Board.ID, f.Whoami.Node, f.Whoami.Author
	}
	for _, o := range []struct{ value, source string }{{env.Getenv("AGORA_BOARD"), "AGORA_BOARD"}, {board, "--board"}} {
		if o.value == "" {
			continue
		}
		if !post.ValidBoard(o.value) {
			return Config{}, fmt.Errorf("%s: invalid board %q: want 1 to 32 lowercase letters, digits and '-', starting with a letter", o.source, o.value)
		}
		c.Board = o.value
	}
	for _, o := range []struct{ value, source string }{{env.Getenv("AGORA_SERVER"), "AGORA_SERVER"}, {server, "--server"}} {
		if o.value == "" {
			continue
		}
		host, port, err := ParseServer(o.value)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", o.source, err)
		}
		c.Host, c.Port = host, port
	}
	if n := env.Getenv("AGORA_NODE"); n != "" {
		if !post.ValidNode(n) {
			return Config{}, fmt.Errorf("AGORA_NODE: invalid node %q: want 1 to 32 lowercase letters, digits and '-', starting with a letter", n)
		}
		c.Node = n
	}
	if c.Node == "" {
		c.Node = "host"
		if env.Hostname != nil {
			if h, err := env.Hostname(); err == nil {
				c.Node = NodeFromHostname(h)
			}
		}
	}
	return c, nil
}

// Find returns the configuration file that commands use: the one named by
// flag, else by AGORA_CONFIG, else the nearest FileName in env.Dir or one of
// its parents below env.Home, else FileName in env.Home. It returns "" if
// there is none.
func Find(flag string, env Env) (string, error) {
	for _, named := range []struct{ path, source string }{{flag, "--config"}, {env.Getenv("AGORA_CONFIG"), "AGORA_CONFIG"}} {
		if named.path == "" {
			continue
		}
		info, err := os.Stat(named.path)
		if err != nil {
			return "", fmt.Errorf("%s: %w", named.source, err)
		}
		if info.IsDir() {
			return "", oldBoard(named.path)
		}
		return named.path, nil
	}
	if env.Dir != "" {
		var home os.FileInfo
		if env.Home != "" {
			home, _ = os.Stat(env.Home)
		}
		for d := filepath.Clean(env.Dir); ; d = filepath.Dir(d) {
			// The home directory's file comes last; what is above it is
			// never searched.
			if home != nil {
				if info, err := os.Stat(d); err == nil && os.SameFile(info, home) {
					break
				}
			}
			if found, err := candidate(filepath.Join(d, FileName)); err != nil || found {
				return filepath.Join(d, FileName), err
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	if env.Home != "" {
		path := filepath.Join(env.Home, FileName)
		if found, err := candidate(path); err != nil || found {
			return path, err
		}
	}
	return "", nil
}

// candidate reports whether path is a configuration file. A path that can't
// be examined, as one that a sandbox hides, counts as absent; a directory is
// agora 1's board.
func candidate(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, fs.ErrPermission) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, oldBoard(path)
	}
	return true, nil
}

func oldBoard(path string) error {
	return fmt.Errorf("%s is a directory, a board of agora 1, which agora 2 can't read: "+
		"agora 2 keeps posts on a server and %s is its configuration file. Move the directory away, then run agora init", path, FileName)
}

// Read reads and checks a configuration file.
func Read(path string) (File, error) {
	var f File
	md, err := toml.DecodeFile(path, &f)
	if err != nil {
		return f, fmt.Errorf("%s: %w", path, err)
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		var names []string
		for _, k := range keys {
			names = append(names, k.String())
		}
		return f, fmt.Errorf("%s: unknown keys %s: see agora's api/config.md", path, strings.Join(names, ", "))
	}
	if md.IsDefined("server", "host") {
		if err := checkHost(f.Server.Host); err != nil {
			return f, fmt.Errorf("%s: [server] host: %w", path, err)
		}
	}
	if md.IsDefined("server", "port") && (f.Server.Port < 1 || f.Server.Port > 65535) {
		return f, fmt.Errorf("%s: [server] port %d: want 1 to 65535", path, f.Server.Port)
	}
	if md.IsDefined("board", "id") && !post.ValidBoard(f.Board.ID) {
		return f, fmt.Errorf("%s: [board] id %q: want 1 to 32 lowercase letters, digits and '-', starting with a letter", path, f.Board.ID)
	}
	if md.IsDefined("whoami", "node") && !post.ValidNode(f.Whoami.Node) {
		return f, fmt.Errorf("%s: [whoami] node %q: want 1 to 32 lowercase letters, digits and '-', starting with a letter", path, f.Whoami.Node)
	}
	if md.IsDefined("whoami", "author") {
		if _, _, err := post.ParseAuthor(f.Whoami.Author); err != nil {
			return f, fmt.Errorf("%s: [whoami] author: %w", path, err)
		}
	}
	return f, nil
}

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*\.?$`)

// checkHost checks a host name or IP address, without a port or brackets.
func checkHost(h string) error {
	if net.ParseIP(h) != nil || (hostPattern.MatchString(h) && len(h) <= 253) {
		return nil
	}
	return fmt.Errorf("invalid host %q: want a host name or an IP address, without a scheme, a port, or brackets", h)
}

// ParseServer parses HOST:PORT, where an IPv6 HOST is in brackets.
func ParseServer(s string) (string, int, error) {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, fmt.Errorf("invalid server %q: want HOST:PORT, such as 127.0.0.1:7777", s)
	}
	if err := checkHost(host); err != nil {
		return "", 0, fmt.Errorf("invalid server %q: %w", s, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", 0, fmt.Errorf("invalid server %q: the port must be from 1 to 65535", s)
	}
	return host, n, nil
}

// NodeFromHostname derives a node from a host name: lowercased, up to its
// first '.', with only a-z, 0-9 and '-', at most 32 characters, or "host" if
// that isn't a valid node.
func NodeFromHostname(h string) string {
	h, _, _ = strings.Cut(strings.ToLower(h), ".")
	var b strings.Builder
	for _, r := range h {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		}
	}
	n := b.String()
	if len(n) > 32 {
		n = n[:32]
	}
	if !post.ValidNode(n) {
		return "host"
	}
	return n
}

// Write writes a configuration file at path with the server host:port and,
// when they aren't empty, board, node, and author. It fails if path exists,
// unless force replaces a file; it never replaces a directory.
func Write(path, host string, port int, board, node, author string, force bool) error {
	if err := checkHost(host); err != nil {
		return err
	}
	if board != "" && !post.ValidBoard(board) {
		return fmt.Errorf("invalid board %q: want 1 to 32 lowercase letters, digits and '-', starting with a letter", board)
	}
	if node != "" && !post.ValidNode(node) {
		return fmt.Errorf("invalid node %q: want 1 to 32 lowercase letters, digits and '-', starting with a letter", node)
	}
	if author != "" {
		if _, _, err := post.ParseAuthor(author); err != nil {
			return err
		}
	}
	var b strings.Builder
	b.WriteString("# agora's configuration; see api/config.md in agora's sources.\n\n[server]\n")
	fmt.Fprintf(&b, "host = %s\nport = %d\n", strconv.Quote(host), port)
	if board != "" {
		fmt.Fprintf(&b, "\n[board]\nid = %s\n", strconv.Quote(board))
	}
	if node != "" || author != "" {
		b.WriteString("\n[whoami]\n")
		if node != "" {
			fmt.Fprintf(&b, "node = %s\n", strconv.Quote(node))
		}
		if author != "" {
			fmt.Fprintf(&b, "author = %s\n", strconv.Quote(author))
		}
	}
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		return fmt.Errorf("%s is a directory: init never replaces one; agora 1's boards were directories, so move it away first", path)
	case err == nil && !force:
		return fmt.Errorf("%s exists: edit it, or pass --force to replace it", path)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// StateDir returns agora's state directory: $XDG_STATE_HOME/agora if that is
// absolute, else %LOCALAPPDATA%\agora on Windows, else
// ~/.local/state/agora.
func StateDir(getenv func(string) string, goos, home string) (string, error) {
	if x := getenv("XDG_STATE_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "agora"), nil
	}
	if goos == "windows" {
		if l := getenv("LOCALAPPDATA"); l != "" {
			return filepath.Join(l, "agora"), nil
		}
		return "", errors.New("can't find the state directory: LOCALAPPDATA isn't set")
	}
	if home == "" {
		return "", errors.New("can't find the state directory: the home directory is unknown")
	}
	return filepath.Join(home, ".local", "state", "agora"), nil
}
