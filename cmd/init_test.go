package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/ui"
)

func TestWriteGitignore(t *testing.T) {
	dir := t.TempDir()

	created, err := writeGitignore(dir)
	if err != nil {
		t.Fatalf("writeGitignore: %v", err)
	}
	if !created {
		t.Fatal("expected file to be created")
	}

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty .gitignore")
	}
}

func TestWriteGitignoreSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	os.WriteFile(path, []byte("custom\n"), 0644)

	created, err := writeGitignore(dir)
	if err != nil {
		t.Fatalf("writeGitignore: %v", err)
	}
	if created {
		t.Fatal("should not create when file exists")
	}

	data, _ := os.ReadFile(path)
	if string(data) != "custom\n" {
		t.Fatal("existing file should not be modified")
	}
}

func TestWriteReadme(t *testing.T) {
	dir := t.TempDir()

	created, err := writeReadme(dir)
	if err != nil {
		t.Fatalf("writeReadme: %v", err)
	}
	if !created {
		t.Fatal("expected file to be created")
	}

	data, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	expected := "# " + filepath.Base(dir) + "\n"
	if string(data) != expected {
		t.Fatalf("expected %q, got %q", expected, string(data))
	}
}

func TestWriteReadmeSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	os.WriteFile(path, []byte("# My Project\n"), 0644)

	created, err := writeReadme(dir)
	if err != nil {
		t.Fatalf("writeReadme: %v", err)
	}
	if created {
		t.Fatal("should not create when file exists")
	}

	data, _ := os.ReadFile(path)
	if string(data) != "# My Project\n" {
		t.Fatal("existing file should not be modified")
	}
}

func TestBootstrapNonInteractive(t *testing.T) {
	dir := t.TempDir()
	r := &git.Runner{Dir: dir, Debug: false, Verify: true}
	r.Init()
	r.RunGit("config", "user.email", "test@test.com")
	r.RunGit("config", "user.name", "Test")

	if err := bootstrapNonInteractive(r); err != nil {
		t.Fatalf("bootstrapNonInteractive: %v", err)
	}

	if r.IsHeadUnborn() {
		t.Fatal("HEAD should not be unborn after bootstrap")
	}
}

func TestBootstrapNonInteractiveWithTrunk(t *testing.T) {
	dir := t.TempDir()
	r := &git.Runner{Dir: dir, Debug: false, Verify: true}
	r.Init()
	r.RunGit("config", "user.email", "test@test.com")
	r.RunGit("config", "user.name", "Test")

	oldTrunk := initFlagTrunk
	initFlagTrunk = "develop"
	defer func() { initFlagTrunk = oldTrunk }()

	if err := bootstrapNonInteractive(r); err != nil {
		t.Fatalf("bootstrapNonInteractive: %v", err)
	}

	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "develop" {
		t.Fatalf("expected branch 'develop', got %q", branch)
	}
}

func TestApplyFormResultCreatesFiles(t *testing.T) {
	dir := t.TempDir()
	r := &git.Runner{Dir: dir, Debug: false, Verify: true}
	r.Init()
	r.RunGit("config", "user.email", "test@test.com")
	r.RunGit("config", "user.name", "Test")

	result := &ui.FormResult{
		Values:  map[string]string{"name": "", "email": "", "branch": "main", "origin": "", "upstream": ""},
		Toggles: map[string]bool{"gitignore": true, "readme": true},
	}

	if err := applyFormResult(r, dir, result); err != nil {
		t.Fatalf("applyFormResult: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
		t.Fatal(".gitignore should exist")
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal("README.md should exist")
	}
	if r.IsHeadUnborn() {
		t.Fatal("should have created a commit")
	}
}

func TestApplyFormResultNoFiles(t *testing.T) {
	dir := t.TempDir()
	r := &git.Runner{Dir: dir, Debug: false, Verify: true}
	r.Init()
	r.RunGit("config", "user.email", "test@test.com")
	r.RunGit("config", "user.name", "Test")

	result := &ui.FormResult{
		Values:  map[string]string{"name": "", "email": "", "branch": "main", "origin": "", "upstream": ""},
		Toggles: map[string]bool{"gitignore": false, "readme": false},
	}

	if err := applyFormResult(r, dir, result); err != nil {
		t.Fatalf("applyFormResult: %v", err)
	}

	if r.IsHeadUnborn() {
		t.Fatal("should have created an empty commit")
	}
}

func TestApplyFormResultSetsConfig(t *testing.T) {
	dir := t.TempDir()
	r := &git.Runner{Dir: dir, Debug: false, Verify: true}
	r.Init()
	r.RunGit("config", "user.email", "test@test.com")
	r.RunGit("config", "user.name", "Test")

	result := &ui.FormResult{
		Values:  map[string]string{"name": "Bob", "email": "bob@test.com", "branch": "main", "origin": "", "upstream": ""},
		Toggles: map[string]bool{"gitignore": false, "readme": false},
	}

	if err := applyFormResult(r, dir, result); err != nil {
		t.Fatalf("applyFormResult: %v", err)
	}

	name, _ := r.GetConfig("user.name")
	if name != "Bob" {
		t.Fatalf("expected user.name 'Bob', got %q", name)
	}
	email, _ := r.GetConfig("user.email")
	if email != "bob@test.com" {
		t.Fatalf("expected user.email 'bob@test.com', got %q", email)
	}
}

func TestApplyFormResultAddsRemotes(t *testing.T) {
	dir := t.TempDir()
	r := &git.Runner{Dir: dir, Debug: false, Verify: true}
	r.Init()
	r.RunGit("config", "user.email", "test@test.com")
	r.RunGit("config", "user.name", "Test")

	result := &ui.FormResult{
		Values: map[string]string{
			"name": "", "email": "", "branch": "main",
			"origin":   "https://github.com/test/repo.git",
			"upstream": "https://github.com/upstream/repo.git",
		},
		Toggles: map[string]bool{"gitignore": false, "readme": false},
	}

	if err := applyFormResult(r, dir, result); err != nil {
		t.Fatalf("applyFormResult: %v", err)
	}

	remotes, _ := r.ListRemotes()
	if len(remotes) != 2 {
		t.Fatalf("expected 2 remotes, got %d: %v", len(remotes), remotes)
	}
}

func TestApplyFormResultCustomBranch(t *testing.T) {
	dir := t.TempDir()
	r := &git.Runner{Dir: dir, Debug: false, Verify: true}
	r.Init()
	r.RunGit("config", "user.email", "test@test.com")
	r.RunGit("config", "user.name", "Test")

	result := &ui.FormResult{
		Values:  map[string]string{"name": "", "email": "", "branch": "develop", "origin": "", "upstream": ""},
		Toggles: map[string]bool{"gitignore": false, "readme": false},
	}

	if err := applyFormResult(r, dir, result); err != nil {
		t.Fatalf("applyFormResult: %v", err)
	}

	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "develop" {
		t.Fatalf("expected branch 'develop', got %q", branch)
	}

	if r.IsHeadUnborn() {
		t.Fatal("should have created a commit")
	}
}
