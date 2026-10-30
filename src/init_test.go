package main

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	bundled "prop-review/skills"
)

func initTestGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func initTestWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func initTestRepo(t *testing.T) (root, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	root = t.TempDir()
	initTestGit(t, root, "init", "-q")
	t.Chdir(root)
	return root, home
}

type initTestEntry struct {
	Mode    fs.FileMode
	ModTime time.Time
	Content string
}

// Include directories and symlinks (without following them), so a failed
// preflight cannot silently create parents or alter an existing destination.
func initTestTree(t *testing.T, root string) map[string]initTestEntry {
	t.Helper()
	entries := make(map[string]initTestEntry)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := initTestEntry{Mode: info.Mode(), ModTime: info.ModTime()}
		if info.Mode()&os.ModeSymlink != 0 {
			value.Content, err = os.Readlink(path)
		} else if info.Mode().IsRegular() {
			var data []byte
			data, err = os.ReadFile(path)
			value.Content = string(data)
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		entries[rel] = value
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func initTestSkills(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	for _, base := range []string{".pi/skills", ".agents/skills"} {
		for _, name := range []string{"prop-review", "prop-review-ingest"} {
			path := filepath.Join(root, base, name, "SKILL.md")
			want, err := bundled.Files.ReadFile(name + "/SKILL.md")
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("%s differs from embedded skill: %v", path, err)
			}
			paths = append(paths, path)
		}
	}
	return paths
}

func TestInitRootAndSubdirectory(t *testing.T) {
	for _, subdir := range []string{".", "nested/deep"} {
		t.Run(subdir, func(t *testing.T) {
			root, home := initTestRepo(t)
			cwd := filepath.Join(root, subdir)
			if err := os.MkdirAll(cwd, 0755); err != nil {
				t.Fatal(err)
			}
			t.Chdir(cwd)
			ignore := filepath.Join(root, ".gitignore")
			initTestWrite(t, ignore, "# user rules\n*.log\n")
			review := filepath.Join(root, "prop-review-tmp", "review.1", "proposal.review.txt")
			initTestWrite(t, review, "Approval/Comment: Approved\n")
			old := time.Unix(1234567890, 0)
			if err := os.Chtimes(review, old, old); err != nil {
				t.Fatal(err)
			}
			reviewsBefore := initTestTree(t, filepath.Dir(review))
			ignoreBefore := initTestTree(t, ignore)
			homeBefore := initTestTree(t, home)
			if err := run([]string{"init"}); err != nil {
				t.Fatal(err)
			}
			paths := initTestSkills(t, root)
			for _, path := range paths {
				// A fixed old timestamp catches rewrites without relying on sleeps
				// or the filesystem's timestamp resolution.
				if err := os.Chtimes(path, old, old); err != nil {
					t.Fatal(err)
				}
			}
			exclude := initTestGit(t, root, "rev-parse", "--git-path", "info/exclude")
			if !filepath.IsAbs(exclude) {
				exclude = filepath.Join(root, exclude)
			}
			if err := os.Chtimes(exclude, old, old); err != nil {
				t.Fatal(err)
			}
			before := initTestTree(t, root)
			if err := run([]string{"init"}); err != nil {
				t.Fatal(err)
			}
			if after := initTestTree(t, root); !reflect.DeepEqual(before, after) {
				t.Error("second init changed repository contents, modes, or mtimes")
			}
			data, err := os.ReadFile(exclude)
			if err != nil || strings.Count(string(data), "/prop-review-tmp/\n") != 1 {
				t.Errorf("expected exactly one exclude rule: %q, %v", data, err)
			}
			initTestGit(t, root, "check-ignore", "-q", "prop-review-tmp/review.1/proposal.review.txt")
			if !reflect.DeepEqual(reviewsBefore, initTestTree(t, filepath.Dir(review))) {
				t.Error("existing review changed")
			}
			if !reflect.DeepEqual(ignoreBefore, initTestTree(t, ignore)) {
				t.Error(".gitignore changed")
			}
			if !reflect.DeepEqual(homeBefore, initTestTree(t, home)) {
				t.Error("init wrote to the user's home")
			}
		})
	}
}

func TestInitRejectsInvalidArgumentsAndOutsideRepository(t *testing.T) {
	root, home := initTestRepo(t)
	before := initTestTree(t, root)
	for _, args := range [][]string{{"init", "extra"}, {"init", "pi"}, {"init", "--help"}, {"init", "a", "b"}} {
		if err := run(args); err == nil || !strings.Contains(err.Error(), "usage: prop-review init") {
			t.Errorf("run(%q) = %v", args, err)
		}
	}
	if !reflect.DeepEqual(before, initTestTree(t, root)) {
		t.Error("invalid arguments changed repository")
	}
	outside := t.TempDir()
	t.Chdir(outside)
	outsideBefore, homeBefore := initTestTree(t, outside), initTestTree(t, home)
	if err := run([]string{"init"}); err == nil || !strings.Contains(err.Error(), "repository root") {
		t.Fatalf("init outside repository: %v", err)
	}
	if !reflect.DeepEqual(outsideBefore, initTestTree(t, outside)) || !reflect.DeepEqual(homeBefore, initTestTree(t, home)) {
		t.Error("rejected init wrote files")
	}
}

func TestInitConflictsPreflightWithoutWrites(t *testing.T) {
	for _, tc := range []struct {
		name, path, kind string
	}{
		{"different skill", ".agents/skills/prop-review-ingest/SKILL.md", "file"},
		{"skill directory", ".agents/skills/prop-review-ingest/SKILL.md", "dir"},
		{"ancestor file", ".agents/skills", "file"},
		{"skill symlink", ".agents/skills/prop-review-ingest/SKILL.md", "symlink-file"},
		{"ancestor symlink", ".agents/skills", "symlink-dir"},
		{"dangling skill symlink", ".agents/skills/prop-review-ingest/SKILL.md", "dangling"},
		{"dangling ancestor symlink", ".agents/skills", "dangling"},
		{"review file", "prop-review-tmp", "file"},
		{"review symlink", "prop-review-tmp", "symlink-dir"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, home := initTestRepo(t)
			path := filepath.Join(root, tc.path)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			switch tc.kind {
			case "file":
				initTestWrite(t, path, "user-owned content\n")
			case "dir":
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatal(err)
				}
			default:
				target := filepath.Join(home, "target")
				if tc.kind == "symlink-file" {
					// Even a symlink to identical bytes must not be accepted.
					data, err := bundled.Files.ReadFile("prop-review-ingest/SKILL.md")
					if err != nil {
						t.Fatal(err)
					}
					initTestWrite(t, target, string(data))
				} else if tc.kind == "symlink-dir" {
					if err := os.Mkdir(target, 0755); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			before, homeBefore := initTestTree(t, root), initTestTree(t, home)
			if err := run([]string{"init"}); err == nil {
				t.Fatal("expected conflicting destination to be rejected")
			}
			if !reflect.DeepEqual(before, initTestTree(t, root)) {
				t.Error("conflict overwrote files or partially created installation")
			}
			if !reflect.DeepEqual(homeBefore, initTestTree(t, home)) {
				t.Error("conflict wrote through symlink into home")
			}
		})
	}
}

func TestInitUsesExistingIgnoreRule(t *testing.T) {
	root, _ := initTestRepo(t)
	initTestWrite(t, filepath.Join(root, ".gitignore"), "/prop-review-tmp/\n")
	ignoreBefore := initTestTree(t, filepath.Join(root, ".gitignore"))
	exclude := filepath.Join(root, ".git", "info", "exclude")
	before := initTestTree(t, exclude)
	if err := run([]string{"init"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, initTestTree(t, exclude)) || !reflect.DeepEqual(ignoreBefore, initTestTree(t, filepath.Join(root, ".gitignore"))) {
		t.Error("init changed ignore files despite existing exclusion")
	}
}

func TestInitLinkedWorktreeExclude(t *testing.T) {
	root, home := initTestRepo(t)
	initTestGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "initial")
	worktree := filepath.Join(t.TempDir(), "linked")
	initTestGit(t, root, "worktree", "add", "--detach", "-q", worktree)
	t.Chdir(worktree)
	homeBefore := initTestTree(t, home)
	if err := run([]string{"init"}); err != nil {
		t.Fatal(err)
	}
	initTestSkills(t, worktree)
	initTestGit(t, worktree, "check-ignore", "-q", "prop-review-tmp/probe")
	exclude := initTestGit(t, worktree, "rev-parse", "--git-path", "info/exclude")
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(worktree, exclude)
	}
	before := initTestTree(t, exclude)
	if err := run([]string{"init"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, initTestTree(t, exclude)) {
		t.Error("worktree init duplicated or rewrote exclude rule")
	}
	for _, dir := range []string{root, worktree} {
		if _, err := os.Lstat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
			t.Errorf("init created .gitignore in %s: %v", dir, err)
		}
	}
	if !reflect.DeepEqual(homeBefore, initTestTree(t, home)) {
		t.Error("worktree init wrote to home")
	}
}

func TestFindIngestSkillPrecedence(t *testing.T) {
	for _, engine := range []string{"pi", "fx"} {
		t.Run(engine, func(t *testing.T) {
			root, home := initTestRepo(t)
			localAgents := filepath.Join(root, ".agents", "skills", "prop-review-ingest", "SKILL.md")
			localPi := filepath.Join(root, ".pi", "skills", "prop-review-ingest", "SKILL.md")
			paths := []string{localAgents, localPi,
				filepath.Join(home, ".agents", "skills", "prop-review-ingest", "SKILL.md"),
				filepath.Join(home, ".pi", "agent", "skills", "prop-review-ingest", "SKILL.md")}
			if engine == "pi" {
				paths[0], paths[1] = paths[1], paths[0]
			}
			for _, path := range paths {
				initTestWrite(t, path, "# skill\n")
			}
			for _, want := range paths {
				got, err := findIngestSkill(root, engine)
				if err != nil || got != want {
					t.Fatalf("findIngestSkill = %q, %v; want %q", got, err, want)
				}
				if err := os.Remove(want); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := findIngestSkill(root, engine); got != "" || err == nil || !strings.Contains(err.Error(), "skill not found; run prop-review init") {
				t.Fatalf("missing skill = %q, %v", got, err)
			}
		})
	}
}

func TestIngestLaunchesRepositoryLocalSkill(t *testing.T) {
	for _, engine := range []string{"pi", "fx"} {
		t.Run(engine, func(t *testing.T) {
			root, log := ingestFixture(t)
			// Git canonicalizes the repository path (notably /var on macOS).
			root, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := run([]string{"init"}); err != nil {
				t.Fatal(err)
			}
			base := ".agents/skills"
			if engine == "pi" {
				base = ".pi/skills"
			}
			want := filepath.Join(root, base, "prop-review-ingest", "SKILL.md")
			if err := run([]string{"ingest", engine}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "Read and follow the skill at "+want+" to report") || strings.Contains(string(data), filepath.Join(os.Getenv("HOME"), ".pi", "agent", "skills")) {
				t.Fatalf("ingest did not select repository-local skill: %s", data)
			}
			if engine == "pi" && !strings.Contains(string(data), "--skill\n"+want+"\n") {
				t.Fatalf("pi --skill did not use local path: %s", data)
			}
			if engine == "fx" && !strings.Contains(string(data), "--resume\ningest-created-id\n") {
				t.Fatalf("fx did not resume created session: %s", data)
			}
		})
	}
}
