package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func doctorTestSetup(t *testing.T, engine string) (string, string) {
	t.Helper()
	root, home := initTestRepo(t)
	if err := setEngine(engine); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for _, name := range []string{"fx", "gh", "node", "pi"} {
		// Merely locating dependencies must never execute engines or auth checks.
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho unexpected invocation >&2\nexit 99\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	initTestWrite(t, filepath.Join(root, ".gitignore"), "/prop-review-tmp/\n")
	return root, home
}

func doctorTestReadOnly(t *testing.T, root, home string, want string) {
	t.Helper()
	before, config := initTestTree(t, root), initTestTree(t, home)
	var out bytes.Buffer
	err := doctor(root, &out)
	if (want == "" && err != nil) || (want != "" && (err == nil || !strings.Contains(out.String(), want))) {
		t.Fatalf("doctor = %v, want %q; output:\n%s", err, want, &out)
	}
	if !reflect.DeepEqual(before, initTestTree(t, root)) || !reflect.DeepEqual(config, initTestTree(t, home)) {
		t.Fatal("doctor changed repository (including Git index/config/exclude) or user configuration")
	}
}

func TestDoctorReadOnly(t *testing.T) {
	for _, state := range []string{"empty", "pending", "reviewed", "pending child", "corrupt", "branch", "cycle", "orphan", "invalid link", "marker", "snapshot", "tracked", "negated", "invalid engine"} {
		t.Run(state, func(t *testing.T) {
			root, home := doctorTestSetup(t, "fx")
			want := ""
			dir := filepath.Join(root, "prop-review-tmp", "review.a")
			if state != "empty" {
				initTestWrite(t, filepath.Join(dir, "proposal.txt"), render([]item{{topic: "topic"}}))
				initTestWrite(t, filepath.Join(dir, "request.txt"), "request")
			}
			switch state {
			case "reviewed", "pending child", "branch":
				initTestWrite(t, filepath.Join(dir, "proposal.review.txt"), render([]item{{topic: "topic", answer: "Approved"}}))
				if state != "reviewed" {
					for _, name := range []string{"review.b", "review.c"} {
						child := filepath.Join(filepath.Dir(dir), name)
						initTestWrite(t, filepath.Join(child, "proposal.txt"), render([]item{{topic: "question"}}))
						initTestWrite(t, filepath.Join(child, "parent.txt"), "review.a\n")
						initTestWrite(t, filepath.Join(child, "followup.txt"), "followup\n")
						if state == "pending child" {
							break
						}
					}
				}
				if state == "branch" {
					want = "ambiguous review branches"
				}
			case "corrupt":
				initTestWrite(t, filepath.Join(dir, "proposal.review.txt"), "bad review")
				want = "review result does not match"
			case "cycle":
				initTestWrite(t, filepath.Join(dir, "parent.txt"), "review.a\n")
				want = "cyclic review history"
			case "orphan":
				initTestWrite(t, filepath.Join(dir, "parent.txt"), "review.missing\n")
				want = "missing or invalid parent"
			case "invalid link":
				initTestWrite(t, filepath.Join(dir, "parent.txt"), "../elsewhere\n")
				want = "invalid parent.txt"
			case "marker":
				initTestWrite(t, filepath.Join(dir, "revision.txt"), "bad\n")
				want = "invalid revision.txt"
			case "snapshot":
				initTestWrite(t, filepath.Join(dir, "issue.json"), "{}")
				want = "missing required issue field"
			case "tracked":
				initTestGit(t, root, "add", "-f", filepath.Join(dir, "request.txt"))
				want = "path is not ignored or is tracked"
			case "negated":
				initTestWrite(t, filepath.Join(root, ".gitignore"), "!/prop-review-tmp/\n!/prop-review-tmp/**\n")
				want = "Git exclusion not confirmed"
			case "invalid engine":
				path, _ := enginePath()
				initTestWrite(t, path, "invalid\n")
				want = "invalid engine setting"
			}
			doctorTestReadOnly(t, root, home, want)
		})
	}
}

func TestDoctorUnsafePaths(t *testing.T) {
	for _, kind := range []string{"base link", "base file", "directory link", "file link"} {
		t.Run(kind, func(t *testing.T) {
			root, home := doctorTestSetup(t, "fx")
			external := t.TempDir()
			initTestWrite(t, filepath.Join(external, "sentinel"), "untouched")
			base := filepath.Join(root, "prop-review-tmp")
			path := base
			switch kind {
			case "base file":
				initTestWrite(t, base, "not a directory")
			case "directory link":
				path = filepath.Join(base, "review.a")
			case "file link":
				path = filepath.Join(base, "review.a", "proposal.txt")
			}
			if kind != "base file" {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, path); err != nil {
					t.Fatal(err)
				}
			}
			before := initTestTree(t, external)
			doctorTestReadOnly(t, root, home, "unsafe review")
			if !reflect.DeepEqual(before, initTestTree(t, external)) {
				t.Fatal("external data changed")
			}
		})
	}
}

func TestDoctorPiSDK(t *testing.T) {
	for _, state := range []string{"present", "missing", "bad root", "npm failure"} {
		t.Run(state, func(t *testing.T) {
			root, home := doctorTestSetup(t, "pi")
			modules := t.TempDir()
			script := "#!/bin/sh\n[ \"$*\" = 'root -g' ] || exit 99\n[ \"$npm_config_logs_max\" = 0 ] || exit 98\nprintf '%s\\n' '" + modules + "'\n"
			want := ""
			switch state {
			case "present":
				initTestWrite(t, filepath.Join(modules, "@earendil-works", "pi-coding-agent", "dist", "index.js"), "throw new Error('must not import SDK');")
			case "missing":
				want = "WARN Pi SDK placement"
			case "bad root":
				script = "#!/bin/sh\necho relative\n"
				want = "absolute module directory"
			case "npm failure":
				script = "#!/bin/sh\nexit 12\n"
				want = "npm root -g"
			}
			bin := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))[0]
			if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			doctorTestReadOnly(t, root, home, want)
		})
	}
}

func TestDoctorMissingCommand(t *testing.T) {
	root, home := doctorTestSetup(t, "fx")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	doctorTestReadOnly(t, root, home, "WARN command fx")
}

func TestDoctorUnsafeEngineSetting(t *testing.T) {
	root, home := doctorTestSetup(t, "fx")
	path, _ := enginePath()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(home, path); err != nil {
		t.Fatal(err)
	}
	doctorTestReadOnly(t, root, home, "unsafe engine setting")
}

func TestDoctorUsage(t *testing.T) {
	if err := run([]string{"doctor", "--fix"}); err == nil || err.Error() != "usage: prop-review doctor" {
		t.Fatalf("unexpected usage: %v", err)
	}
}
