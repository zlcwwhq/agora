package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
)

// transcriptTail is how much of a transcript, from its end, detection reads
// for a model.
const transcriptTail = 16 << 20

// detectedModel returns the model in the transcript of a detected session,
// as api/cli.md's Models says, or "" if there is none. It never fails and
// never writes: what it finds labels posts, and an agent reads whatever a
// command writes to standard error.
func (c *ctx) detectedModel(harness, session string) string {
	if session == "." || session == ".." {
		return ""
	}
	var paths []string
	var pick func(line []byte) string
	switch harness {
	case "claude-code":
		pick = claudeCodeModel
		for _, dir := range c.harnessDirs("CLAUDE_CONFIG_DIR", ".claude") {
			for _, project := range subdirs(filepath.Join(dir, "projects"), 1) {
				paths = append(paths, filepath.Join(project, session+".jsonl"))
			}
		}
	case "codex":
		pick = codexModel
		for _, dir := range c.harnessDirs("CODEX_HOME", ".codex") {
			for _, day := range subdirs(filepath.Join(dir, "sessions"), 3) {
				entries, _ := os.ReadDir(day)
				for _, e := range entries {
					// rollout-<time>-<thread>.jsonl, or, after a revert,
					// rollout-<time>-<thread>_<rollout>.jsonl.
					if n := e.Name(); strings.HasPrefix(n, "rollout-") && strings.HasSuffix(n, ".jsonl") &&
						(strings.HasSuffix(n, "-"+session+".jsonl") || strings.Contains(n, "-"+session+"_")) {
						paths = append(paths, filepath.Join(day, n))
					}
				}
			}
		}
	case "antigravity":
		pick = antigravityModel
		// The first of the CLI's, the desktop app's, and the IDE's
		// directories that holds the conversation.
		for _, dir := range c.harnessDirs("ANTIGRAVITY_APP_DATA_DIR",
			filepath.Join(".gemini", "antigravity-cli"), filepath.Join(".gemini", "antigravity"), filepath.Join(".gemini", "antigravity-ide")) {
			path := filepath.Join(dir, "brain", session, ".system_generated", "logs", "transcript.jsonl")
			if _, err := os.Stat(path); err == nil {
				paths = []string{path}
				break
			}
		}
	}
	return lastModel(newest(paths), pick)
}

// harnessDirs returns the harness's directories: the variable's value if it
// is an absolute path, else the defaults in the home directory if that is
// absolute, so that none is relative to wherever agora runs.
func (c *ctx) harnessDirs(variable string, defaults ...string) []string {
	if d := c.env.Getenv(variable); filepath.IsAbs(d) {
		return []string{d}
	}
	home := c.home()
	if !filepath.IsAbs(home) {
		return nil
	}
	dirs := make([]string, len(defaults))
	for i, d := range defaults {
		dirs[i] = filepath.Join(home, d)
	}
	return dirs
}

// subdirs returns the directories depth levels below dir. It reads the
// directories rather than globbing, so that a directory's name may hold
// characters that a pattern would take for its own, such as [.
func subdirs(dir string, depth int) []string {
	dirs := []string{dir}
	for range depth {
		var below []string
		for _, d := range dirs {
			entries, _ := os.ReadDir(d)
			for _, e := range entries {
				if e.IsDir() {
					below = append(below, filepath.Join(d, e.Name()))
				}
			}
		}
		dirs = below
	}
	return dirs
}

// newest returns the most recently modified of the regular files at paths,
// or "" if there is none.
func newest(paths []string) string {
	var path string
	var latest time.Time
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && (path == "" || fi.ModTime().After(latest)) {
			path, latest = p, fi.ModTime()
		}
	}
	return path
}

// lastModel returns the model of the last line, within the file's last
// transcriptTail bytes, for which pick finds a valid one, or "" if there is
// none or the file can't be read. It reads backwards, in chunks that
// double, so that a recent model costs little and lines of any length fit.
func lastModel(path string, pick func(line []byte) string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	end := fi.Size()
	floor := max(0, end-transcriptTail)
	try := func(line []byte) string {
		if m := pick(line); m != "" && post.ValidModel(m) {
			return m
		}
		return ""
	}
	// partial is what the chunks read so far start with: the end of a line
	// that starts before end.
	var partial []byte
	for n := int64(64 << 10); end > floor; n *= 2 {
		off := max(floor, end-n)
		buf := make([]byte, int(end-off), int(end-off)+len(partial))
		if _, err := f.ReadAt(buf, off); err != nil {
			return ""
		}
		buf = append(buf, partial...)
		end = off
		for i := bytes.LastIndexByte(buf, '\n'); i >= 0; i = bytes.LastIndexByte(buf, '\n') {
			if m := try(buf[i+1:]); m != "" {
				return m
			}
			buf = buf[:i]
		}
		partial = buf
	}
	if floor == 0 {
		// The file's first line; past the tail, a line is cut.
		return try(partial)
	}
	return ""
}

// claudeCodeModel picks the model of a response of Claude Code's own, not
// of a subagent nor one that no model gave.
func claudeCodeModel(line []byte) string {
	if !bytes.Contains(line, []byte(`"assistant"`)) {
		return ""
	}
	var l struct {
		Type        string `json:"type"`
		IsSidechain bool   `json:"isSidechain"`
		Message     struct {
			Model string `json:"model"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &l) != nil || l.Type != "assistant" || l.IsSidechain || l.Message.Model == "<synthetic>" {
		return ""
	}
	return l.Message.Model
}

// codexModel picks the model of a turn of Codex.
func codexModel(line []byte) string {
	if !bytes.Contains(line, []byte(`"turn_context"`)) {
		return ""
	}
	var l struct {
		Type    string `json:"type"`
		Payload struct {
			Model string `json:"model"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &l) != nil || l.Type != "turn_context" {
		return ""
	}
	return l.Payload.Model
}

// antigravityModel picks the model of a response of Antigravity.
func antigravityModel(line []byte) string {
	if !bytes.Contains(line, []byte(`"PLANNER_RESPONSE"`)) {
		return ""
	}
	var l struct {
		Type  string `json:"type"`
		Model string `json:"model"`
	}
	if json.Unmarshal(line, &l) != nil || l.Type != "PLANNER_RESPONSE" {
		return ""
	}
	return l.Model
}
