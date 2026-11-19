package board

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// DirName is the name of a board's directory, which init creates in the
// working directory and commands search for.
const DirName = ".agora"

// Find returns the board that commands use from the working directory dir, as
// api/cli.md specifies: the nearest DirName in the search directory or one of
// its parents, else DirName in home. It returns "" if there is none. dir and
// home may be empty when they are unknown, which skips them.
func Find(dir, home string) (string, error) {
	if dir != "" {
		start, err := searchDir(filepath.Clean(dir))
		if err != nil {
			return "", err
		}
		for d := start; ; d = filepath.Dir(d) {
			candidate := filepath.Join(d, DirName)
			if ok, err := isDir(candidate); err != nil || ok {
				return candidate, err
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	if home != "" {
		candidate := filepath.Join(home, DirName)
		if ok, err := isDir(candidate); err != nil || ok {
			return candidate, err
		}
	}
	return "", nil
}

// searchDir returns the directory that the search for a board starts from:
// dir, except in a linked worktree of a repository with a main worktree,
// where it is dir's path relative to the linked worktree applied to the main
// worktree, so that the worktree uses the main worktree's board.
func searchDir(dir string) (string, error) {
	wt, err := LinkedWorktree(dir)
	if err != nil || wt.Main == "" {
		return dir, err
	}
	rel, err := filepath.Rel(wt.Root, dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(wt.Main, rel), nil
}

// Worktree is a linked worktree, one that git worktree add created.
type Worktree struct {
	// Root is the linked worktree's top directory.
	Root string
	// Main is the repository's main worktree, the directory that holds its
	// .git directory, or "" if it has none, as a bare repository doesn't.
	Main string
}

// LinkedWorktree returns the linked worktree that contains path, which needn't
// exist, or a zero Worktree if path isn't in one. It reads Git's files rather
// than running git: the top directory of a linked worktree has a .git file
// that names the worktree's administrative directory, whose commondir file
// names the repository's directory. A submodule's .git file names a
// directory without commondir, so a submodule isn't a linked worktree.
func LinkedWorktree(path string) (Worktree, error) {
	for d := filepath.Clean(path); ; d = filepath.Dir(d) {
		info, err := os.Stat(filepath.Join(d, ".git"))
		switch {
		case err == nil && info.IsDir():
			return Worktree{}, nil
		case err == nil && info.Mode().IsRegular():
			return linkedWorktree(d)
		case err != nil && !absent(err):
			return Worktree{}, err
		}
		if filepath.Dir(d) == d {
			return Worktree{}, nil
		}
	}
}

// linkedWorktree reads the .git file at the top of root.
func linkedWorktree(root string) (Worktree, error) {
	data, err := readFile(filepath.Join(root, ".git"))
	if err != nil {
		return Worktree{}, err
	}
	first, _, _ := strings.Cut(string(data), "\n")
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(first), "gitdir:")
	if !ok {
		return Worktree{}, nil
	}
	admin := gitPath(root, gitDir)
	// A directory that can't be read, as when a sandbox hides the main
	// worktree, mustn't pass for a submodule's, which has no commondir: init
	// would then create a board in the worktree.
	if info, err := os.Stat(admin); err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("not a directory")
		}
		return Worktree{}, fmt.Errorf("can't tell whether %s is a linked worktree: its .git names %s: %w", root, admin, err)
	}
	data, err = readFile(filepath.Join(admin, "commondir"))
	if errors.Is(err, fs.ErrNotExist) {
		return Worktree{}, nil
	}
	if err != nil {
		return Worktree{}, err
	}
	common := gitPath(admin, string(data))
	wt := Worktree{Root: root}
	// A repository with a main worktree keeps its directory as .git in it. A
	// bare repository's directory has another name, such as proj.git.
	if filepath.Base(common) == ".git" {
		wt.Main = filepath.Dir(common)
	}
	return wt, nil
}

// gitPath interprets a path from one of Git's files, which may be relative to
// base and uses slashes on every system.
func gitPath(base, p string) string {
	p = filepath.FromSlash(strings.TrimSpace(p))
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

// isDir reports whether path is a directory. A path that can't be examined,
// such as one that a sandbox hides, counts as absent, so that searches go on.
func isDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if absent(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}

// absent reports whether err means that a path, while searching upwards,
// isn't there for this process: it doesn't exist, a component isn't a
// directory, or a sandbox keeps the process from examining it.
func absent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, fs.ErrPermission)
}
