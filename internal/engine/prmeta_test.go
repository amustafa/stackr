package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A value is inline JSON or a filename, decided on its first character. Both
// forms accept one object or an array, so a caller writing content for a single
// branch is never made to wrap it in brackets.
func TestParsePRMeta_AcceptsInlineAndFileInEitherShape(t *testing.T) {
	dir := t.TempDir()
	arrayFile := filepath.Join(dir, "prs.json")
	if err := os.WriteFile(arrayFile, []byte(`[
		{"branch":"a","title":"First"},
		{"branch":"b","title":"Second"}
	]`), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParsePRMeta([]string{
		`{"branch":"c","title":"Inline object"}`,
		`[{"branch":"d","title":"Inline array"}]`,
		arrayFile,
	})
	if err != nil {
		t.Fatalf("ParsePRMeta: %v", err)
	}

	// Order is the order given, because bindPRMeta reports conflicts by naming
	// entries and a reshuffled list makes those messages point at the wrong one.
	want := []string{"c", "d", "a", "b"}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, branch := range want {
		if entries[i].Branch != branch {
			t.Errorf("entry %d is %q, want %q", i, entries[i].Branch, branch)
		}
	}
}

// bodyFile is resolved at parse time so an unreadable path is reported before
// anything is pushed, rather than partway through creating a stack of PRs.
func TestParsePRMeta_ResolvesBodyFile(t *testing.T) {
	dir := t.TempDir()
	body := filepath.Join(dir, "body.md")
	if err := os.WriteFile(body, []byte("## Summary\n\nBackticks `and` \"quotes\".\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParsePRMeta([]string{`{"branch":"a","title":"T","bodyFile":"` + body + `"}`})
	if err != nil {
		t.Fatalf("ParsePRMeta: %v", err)
	}

	if !strings.Contains(entries[0].Body, "Backticks `and` \"quotes\".") {
		t.Errorf("body = %q, want the file's contents", entries[0].Body)
	}
	// Cleared once folded in, so nothing downstream reads the file a second time
	// and gets a different answer.
	if entries[0].BodyFile != "" {
		t.Errorf("bodyFile = %q, want it cleared once resolved", entries[0].BodyFile)
	}
}

func TestParsePRMeta_Rejects(t *testing.T) {
	tests := map[string]struct {
		values  []string
		wantErr string
	}{
		"missing title":       {[]string{`{"branch":"a"}`}, `no "title"`},
		"body and bodyFile":   {[]string{`{"branch":"a","title":"T","body":"x","bodyFile":"y"}`}, "both"},
		"unreadable bodyFile": {[]string{`{"branch":"a","title":"T","bodyFile":"/nope/nope.md"}`}, "could not read bodyFile"},
		"missing file":        {[]string{"/nope/prs.json"}, "could not read --pr-meta file"},
		"malformed json":      {[]string{`{"branch":`}, "invalid --pr-meta"},
		"empty value":         {[]string{"  "}, "empty value"},
	}

	for name, tc := range tests {
		_, err := ParsePRMeta(tc.values)
		if err == nil {
			t.Errorf("%s: want an error", name)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: error = %q, want it to mention %q", name, err, tc.wantErr)
		}
	}
}

// A branchless entry is only unambiguous when the submit creates exactly one PR.
// Binding it to a branch in any other case would label that branch with another
// branch's summary, which survives review looking deliberate.
func TestBindPRMeta_BranchlessEntry(t *testing.T) {
	submitting := []string{"a", "b", "c"}

	t.Run("binds to the only branch needing a PR", func(t *testing.T) {
		entries := []PRMeta{{Title: "Only one"}}

		// The branch needing a PR is not the one submit was invoked on: submit
		// reaches downstack, so the target frequently already has its PR.
		bound, err := bindPRMeta(entries, submitting, []string{"b"}, "c")
		if err != nil {
			t.Fatalf("bindPRMeta: %v", err)
		}
		if bound["b"] == nil || bound["b"].Title != "Only one" {
			t.Fatalf("bound = %v, want the entry on b", bound)
		}
	})

	t.Run("falls back to the target when nothing needs a PR", func(t *testing.T) {
		bound, err := bindPRMeta([]PRMeta{{Title: "Only one"}}, submitting, nil, "c")
		if err != nil {
			t.Fatalf("bindPRMeta: %v", err)
		}
		if bound["c"] == nil {
			t.Fatalf("bound = %v, want the entry on the target", bound)
		}
	})

	t.Run("errors when several PRs are being created", func(t *testing.T) {
		_, err := bindPRMeta([]PRMeta{{Title: "Only one"}}, submitting, []string{"a", "b"}, "c")
		if err == nil {
			t.Fatal("want an error naming the branches that need a PR")
		}
		for _, want := range []string{"a", "b", `"branch"`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to mention %q", err, want)
			}
		}
	})

	t.Run("errors when two entries leave the branch out", func(t *testing.T) {
		_, err := bindPRMeta([]PRMeta{{Title: "One"}, {Title: "Two"}}, submitting, []string{"a"}, "a")
		if err == nil {
			t.Fatal("want an error: only one entry may omit the branch")
		}
	})

	t.Run("errors when it would collide with a named entry", func(t *testing.T) {
		entries := []PRMeta{{Branch: "b", Title: "Named"}, {Title: "Loose"}}

		_, err := bindPRMeta(entries, submitting, []string{"b"}, "b")
		if err == nil {
			t.Fatal("want an error: the loose entry binds to a branch already named")
		}
	})
}

// Validation is total and happens before any PR is created — a payload that is
// wrong about its third branch must not leave two PRs already made.
func TestBindPRMeta_RejectsBranchesOutsideTheSubmit(t *testing.T) {
	entries := []PRMeta{{Branch: "a", Title: "Fine"}, {Branch: "zz", Title: "Not submitted"}}

	_, err := bindPRMeta(entries, []string{"a", "b"}, []string{"a", "b"}, "b")
	if err == nil {
		t.Fatal("want an error naming the branch that is not being submitted")
	}
	if !strings.Contains(err.Error(), "zz") || !strings.Contains(err.Error(), "a, b") {
		t.Errorf("error = %q, want it to name the offender and list what is being submitted", err)
	}
}

// The pre-push check is the cheap half of validation: it needs no network, so it
// runs before anything is pushed. It must not reject a branchless entry, whose
// ambiguity can only be judged once the PR survey is in.
func TestCheckPRMetaBranches(t *testing.T) {
	set := []string{"a", "b"}

	if err := CheckPRMetaBranches([]PRMeta{{Branch: "a", Title: "T"}, {Title: "Loose"}}, set); err != nil {
		t.Errorf("want no error for a submitted branch and a branchless entry, got %v", err)
	}

	err := CheckPRMetaBranches([]PRMeta{{Branch: "zz", Title: "T"}}, set)
	if err == nil {
		t.Fatal("want an error for a branch outside the submit")
	}
	if !strings.Contains(err.Error(), "zz") {
		t.Errorf("error = %q, want it to name the offender", err)
	}
}

func TestBindPRMeta_RejectsDuplicateBranches(t *testing.T) {
	entries := []PRMeta{{Branch: "a", Title: "First"}, {Branch: "a", Title: "Second"}}

	if _, err := bindPRMeta(entries, []string{"a"}, []string{"a"}, "a"); err == nil {
		t.Fatal("want an error: two entries for one branch")
	}
}

func TestBindPRMeta_NoEntriesBindsNothing(t *testing.T) {
	bound, err := bindPRMeta(nil, []string{"a"}, []string{"a"}, "a")
	if err != nil {
		t.Fatalf("bindPRMeta: %v", err)
	}
	if bound != nil {
		t.Errorf("bound = %v, want nil so every branch falls through to the prompt", bound)
	}
}

// Draft is a pointer so an explicit false can opt one branch out of a --draft
// submit — a bool would make that indistinguishable from saying nothing.
func TestPRMeta_DraftFor(t *testing.T) {
	yes, no := true, false

	tests := map[string]struct {
		meta          *PRMeta
		submitDefault bool
		want          bool
	}{
		"no entry follows the submit":      {nil, true, true},
		"silent entry follows the submit":  {&PRMeta{}, true, true},
		"explicit true overrides":          {&PRMeta{Draft: &yes}, false, true},
		"explicit false overrides --draft": {&PRMeta{Draft: &no}, true, false},
	}

	for name, tc := range tests {
		if got := tc.meta.DraftFor(tc.submitDefault); got != tc.want {
			t.Errorf("%s: DraftFor(%v) = %v, want %v", name, tc.submitDefault, got, tc.want)
		}
	}
}
