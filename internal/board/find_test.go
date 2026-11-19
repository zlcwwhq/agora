package board

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zlcwwhq/agora/internal/testutil"
)

// mkdirs creates directories beneath root, named with slashes.
func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// addWorktree lays out the files that git worktree add leaves for a linked
// worktree at worktree of the repository whose directory is common: an
// administrative directory in common, and a .git file in the worktree that
// names it, relative to the worktree if relative is true, as
// worktree.useRelativePaths makes Git write it.
func addWorktree(t *testing.T, common, worktree string, relative bool) {
	t.Helper()
	admin := filepath.Join(common, "worktrees", filepath.Base(worktree))
	writeFile(t, filepath.Join(admin, "commondir"), "../..\n")
	writeFile(t, filepath.Join(admin, "gitdir"), filepath.ToSlash(filepath.Join(worktree, ".git"))+"\n")
	gitDir := admin
	if relative {
		rel, err := filepath.Rel(worktree, admin)
		if err != nil {
			t.Fatal(err)
		}
		gitDir = rel
	}
	writeFile(t, filepath.Join(worktree, ".git"), "gitdir: "+filepath.ToSlash(gitDir)+"\n")
}

func find(t *testing.T, dir, home string) string {
	t.Helper()
	got, err := Find(dir, home)
	if err != nil {
		t.Fatalf("Find(%s): %v", dir, err)
	}
	return got
}

func TestFindNearestThenHome(t *testing.T) {
	root := testutil.TempDir(t)
	home := filepath.Join(root, "home")
	proj := filepath.Join(home, "src", "proj")
	outside := filepath.Join(root, "elsewhere")
	mkdirs(t, root, "home/src/proj/internal/cli", "elsewhere")

	// A board above the temporary directory, such as a real ~/.agora on
	// Windows, whose %TEMP% is beneath the home directory, is nearer than the
	// test's home. There normally is none.
	ambient := find(t, root, "")
	if got := find(t, proj, home); got != ambient {
		t.Fatalf("without boards: %q", got)
	}
	mkdirs(t, home, DirName)
	want := filepath.Join(home, DirName)
	if ambient != "" {
		want = ambient
	}
	if got := find(t, outside, home); got != want {
		t.Fatalf("outside the home directory: %q, want %q", got, want)
	}
	if got := find(t, outside, ""); got != ambient {
		t.Fatalf("without a home directory: %q", got)
	}

	mkdirs(t, proj, DirName)
	for _, dir := range []string{proj, filepath.Join(proj, "internal", "cli"), filepath.Join(proj, "not", "there")} {
		if got, want := find(t, dir, home), filepath.Join(proj, DirName); got != want {
			t.Errorf("from %s: %q, want the project's board", dir, got)
		}
	}

	// A file named .agora isn't a board.
	writeFile(t, filepath.Join(proj, "internal", DirName), "")
	if got, want := find(t, filepath.Join(proj, "internal", "cli"), home), filepath.Join(proj, DirName); got != want {
		t.Errorf("past a file named .agora: %q", got)
	}
	if got := find(t, "", ""); got != "" {
		t.Errorf("without a working directory: %q", got)
	}
}

func TestFindFromLinkedWorktree(t *testing.T) {
	for _, relative := range []bool{false, true} {
		root := testutil.TempDir(t)
		main := filepath.Join(root, "src", "proj")
		wt := filepath.Join(root, "wt", "fix")
		mkdirs(t, root, "src/proj/.git", "wt/fix/internal/cli")
		addWorktree(t, filepath.Join(main, ".git"), wt, relative)

		w, err := LinkedWorktree(filepath.Join(wt, "internal", "cli"))
		if err != nil || w.Root != wt || w.Main != main {
			t.Fatalf("relative %v: LinkedWorktree = %+v, %v", relative, w, err)
		}

		// The worktree uses its main worktree's board, never one of its own
		// or above it.
		mkdirs(t, root, "src/proj/.agora", "wt/fix/.agora", "wt/.agora")
		if got, want := find(t, filepath.Join(wt, "internal", "cli"), ""), filepath.Join(main, DirName); got != want {
			t.Errorf("relative %v: %q, want the main worktree's board", relative, got)
		}

		// The search goes on from the same place in the main worktree, so a
		// board above the main worktree serves the worktree too.
		if err := os.Remove(filepath.Join(main, DirName)); err != nil {
			t.Fatal(err)
		}
		mkdirs(t, root, "src/.agora")
		if got, want := find(t, filepath.Join(wt, "internal", "cli"), ""), filepath.Join(root, "src", DirName); got != want {
			t.Errorf("relative %v: %q, want the board above the main worktree", relative, got)
		}
	}
}

func TestFindFromBareWorktreeAndSubmodule(t *testing.T) {
	root := testutil.TempDir(t)

	// A bare repository has no main worktree: its worktrees search from
	// where they are.
	bare := filepath.Join(root, "proj.git")
	wt := filepath.Join(root, "wt", "fix")
	mkdirs(t, root, "proj.git", "wt/fix/sub", "wt/.agora")
	addWorktree(t, bare, wt, false)
	if w, err := LinkedWorktree(wt); err != nil || w.Root != wt || w.Main != "" {
		t.Fatalf("bare: LinkedWorktree = %+v, %v", w, err)
	}
	if got, want := find(t, filepath.Join(wt, "sub"), ""), filepath.Join(root, "wt", DirName); got != want {
		t.Errorf("bare: %q, want the board above the worktree", got)
	}

	// A submodule's .git file names a directory without commondir.
	super := filepath.Join(root, "super")
	sub := filepath.Join(super, "lib")
	mkdirs(t, root, "super/.git/modules/lib", "super/lib/src", "super/.agora")
	writeFile(t, filepath.Join(sub, ".git"), "gitdir: ../.git/modules/lib\n")
	if w, err := LinkedWorktree(filepath.Join(sub, "src")); err != nil || w != (Worktree{}) {
		t.Fatalf("submodule: LinkedWorktree = %+v, %v", w, err)
	}
	if got, want := find(t, filepath.Join(sub, "src"), ""), filepath.Join(super, DirName); got != want {
		t.Errorf("submodule: %q, want the superproject's board", got)
	}

	// Neither is a repository's main worktree.
	if w, err := LinkedWorktree(super); err != nil || w != (Worktree{}) {
		t.Fatalf("main worktree: LinkedWorktree = %+v, %v", w, err)
	}

	// A .git file that names a directory that can't be read, as when a
	// sandbox hides the main worktree, isn't taken for a submodule's.
	hidden := filepath.Join(root, "hidden")
	writeFile(t, filepath.Join(hidden, ".git"), "gitdir: "+filepath.ToSlash(filepath.Join(root, "gone", ".git", "worktrees", "hidden"))+"\n")
	if _, err := LinkedWorktree(hidden); err == nil {
		t.Fatal("a .git file naming a missing directory passed for a submodule's")
	}
	if _, err := Find(hidden, ""); err == nil {
		t.Fatal("Find from a worktree whose repository can't be read succeeded")
	}
}

func TestInitWritesGitignore(t *testing.T) {
	b := open(t)
	if err := b.Init("wsl", false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(b.Root, ".gitignore")
	if data, err := os.ReadFile(path); err != nil || string(data) != "*\n" {
		t.Fatalf(".gitignore: %q, %v", data, err)
	}

	// init leaves an existing .gitignore alone, and no scratch file behind.
	if err := os.WriteFile(path, []byte("posts/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := b.Init("wsl", false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "posts/\n" {
		t.Fatalf("changed .gitignore: %q, %v", data, err)
	}
	if left, _ := os.ReadDir(filepath.Join(b.Root, ".tmp")); len(left) != 0 {
		t.Fatalf("scratch files left: %v", left)
	}
}
