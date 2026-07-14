package main

import (
	"strings"
	"testing"
)

func TestParseVersionAndComparison(t *testing.T) {
	current, err := parseVersion("0.4.9")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"0.4", "v0.4.0", "0.4.0-rc1", "1.2.x"} {
		if _, err := parseVersion(value); err == nil {
			t.Errorf("parseVersion(%q) should fail", value)
		}
	}
	for candidate, want := range map[string]bool{
		"0.4.10": true,
		"0.5.0":  true,
		"1.0.0":  true,
		"0.4.9":  false,
		"0.4.8":  false,
	} {
		parsed, err := parseVersion(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if got := parsed.greaterThan(current); got != want {
			t.Errorf("%s greaterThan 0.4.9 = %v, want %v", candidate, got, want)
		}
	}
}

func TestUpdatePackageVersion(t *testing.T) {
	content := `{ buildGoModule }:

buildGoModule rec {
  pname = "gh-skill-tui";
  version = "0.4.0";
}
`
	updated, err := updatePackageVersion(content, "0.5.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, `version = "0.5.0";`) {
		t.Fatalf("updated package = %q", updated)
	}
	for _, value := range []string{"0.4.0", "0.3.0", "next"} {
		if _, err := updatePackageVersion(content, value); err == nil {
			t.Errorf("updatePackageVersion(%q) should fail", value)
		}
	}
}

func TestUpdateAndExtractReleaseNotes(t *testing.T) {
	content := `# Release Notes

## Latest Changes

### Features

* Add the release pipeline. PR #20.

### Fixes

* Keep main stable. PR #21.

## 0.4.0 (2026-07-13)

* Previous release.
`
	updated, err := updateReleaseNotes(content, "0.5.0", "2026-07-14")
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := `# Release Notes

## Latest Changes

## 0.5.0 (2026-07-14)

### Features`
	if !strings.HasPrefix(updated, wantPrefix) {
		t.Fatalf("updated notes:\n%s", updated)
	}
	body, err := releaseNotesForVersion(updated, "0.5.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "Add the release pipeline") || strings.Contains(body, "Previous release") {
		t.Fatalf("release body:\n%s", body)
	}
	if _, err := updateReleaseNotes(updated, "0.5.0", "2026-07-14"); err == nil {
		t.Fatal("duplicate version should fail")
	}
}

func TestEmptyLatestChangesFails(t *testing.T) {
	content := "# Release Notes\n\n## Latest Changes\n\n## 0.4.0\n\n* Previous.\n"
	if _, err := updateReleaseNotes(content, "0.5.0", "2026-07-14"); err == nil {
		t.Fatal("empty Latest Changes should fail")
	}
}
