package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUserFacingPathsAreShortWithoutChangingSelection(t *testing.T) {
	root, proposal, _ := clarificationFixture(t)
	inFixtureRepo(t, root)
	if got := displayPath(root, proposal); got != filepath.Join(filepath.Base(filepath.Dir(proposal)), "proposal.txt") {
		t.Fatalf("proposal path: %q", got)
	}
	if got := displayPath(root, filepath.Join(root, "prop-review-tmp", "flow.txt")); got != "flow.txt" {
		t.Fatalf("flow path: %q", got)
	}
	outside := filepath.Join(t.TempDir(), "external.txt")
	if got := displayPath(root, outside); got != outside {
		t.Fatalf("external path shortened: %q", got)
	}
	var menu strings.Builder
	selected, err := selectProposal([]string{proposal}, strings.NewReader("1\n"), &menu)
	if err != nil || selected != proposal || strings.Contains(menu.String(), root) || !strings.Contains(menu.String(), "proposal.txt") {
		t.Fatalf("proposal menu: %q, %q, %v", menu.String(), selected, err)
	}
	menu.Reset()
	selected, err = selectClarificationRoot([]string{proposal}, strings.NewReader("1\n"), &menu)
	if err != nil || selected != proposal || strings.Contains(menu.String(), root) || !strings.Contains(menu.String(), "元の依頼") {
		t.Fatalf("clarify menu: %q, %q, %v", menu.String(), selected, err)
	}
	menu.Reset()
	if err := deleteProposal(root, strings.NewReader("1\nN\n"), &menu); err == nil || strings.Contains(menu.String(), root) || !strings.Contains(menu.String(), "Delete: "+filepath.Base(filepath.Dir(proposal))) {
		t.Fatalf("delete preview: %q, %v", menu.String(), err)
	}
	if got := displayMessage(root, "invalid "+proposal+" and "+outside); !strings.Contains(got, "review.") || !strings.Contains(got, outside) || strings.Contains(got, root) {
		t.Fatalf("error message: %q", got)
	}
}
