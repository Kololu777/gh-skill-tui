// Command prepare-release updates and reads the files used by the release
// workflows. It deliberately uses only the standard library so that it can run
// on a clean GitHub-hosted runner after actions/setup-go.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const latestChangesHeader = "## Latest Changes"

var (
	packageVersionPattern = regexp.MustCompile(`(?m)^  version = "([0-9]+\.[0-9]+\.[0-9]+)";$`)
	versionPattern        = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

type version struct {
	major int
	minor int
	patch int
}

func parseVersion(value string) (version, error) {
	if !versionPattern.MatchString(value) {
		return version{}, fmt.Errorf("invalid version %q: expected X.Y.Z", value)
	}
	parts := strings.Split(value, ".")
	values := make([]int, len(parts))
	for i, part := range parts {
		parsed, err := strconv.Atoi(part)
		if err != nil {
			return version{}, fmt.Errorf("invalid version %q: %w", value, err)
		}
		values[i] = parsed
	}
	return version{major: values[0], minor: values[1], patch: values[2]}, nil
}

func (v version) greaterThan(other version) bool {
	if v.major != other.major {
		return v.major > other.major
	}
	if v.minor != other.minor {
		return v.minor > other.minor
	}
	return v.patch > other.patch
}

func currentVersion(content string) (string, error) {
	matches := packageVersionPattern.FindAllStringSubmatch(content, -1)
	if len(matches) != 1 {
		return "", fmt.Errorf("expected exactly one package.nix version, found %d", len(matches))
	}
	return matches[0][1], nil
}

func updatePackageVersion(content, next string) (string, error) {
	current, err := currentVersion(content)
	if err != nil {
		return "", err
	}
	currentParsed, err := parseVersion(current)
	if err != nil {
		return "", err
	}
	nextParsed, err := parseVersion(next)
	if err != nil {
		return "", err
	}
	if !nextParsed.greaterThan(currentParsed) {
		return "", fmt.Errorf("release version %s must be greater than %s", next, current)
	}
	return packageVersionPattern.ReplaceAllString(content, `  version = "`+next+`";`), nil
}

func latestChanges(content string) (string, error) {
	headerStart := strings.Index(content, latestChangesHeader)
	if headerStart < 0 {
		return "", fmt.Errorf("release notes do not contain %q", latestChangesHeader)
	}
	contentStart := headerStart + len(latestChangesHeader)
	rest := content[contentStart:]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		rest = rest[:next]
	}
	body := strings.TrimSpace(rest)
	if body == "" {
		return "", errors.New("latest changes is empty; merge at least one recorded change before releasing")
	}
	return body, nil
}

func updateReleaseNotes(content, next, releaseDate string) (string, error) {
	if _, err := parseVersion(next); err != nil {
		return "", err
	}
	if _, err := time.Parse(time.DateOnly, releaseDate); err != nil {
		return "", fmt.Errorf("invalid release date %q: expected YYYY-MM-DD", releaseDate)
	}
	if _, err := latestChanges(content); err != nil {
		return "", err
	}
	headingPattern := regexp.MustCompile(`(?m)^## ` + regexp.QuoteMeta(next) + `(?: \([^)]+\))?$`)
	if headingPattern.MatchString(content) {
		return "", fmt.Errorf("release notes already contain version %s", next)
	}
	headerStart := strings.Index(content, latestChangesHeader)
	headerEnd := headerStart + len(latestChangesHeader)
	after := strings.TrimLeft(content[headerEnd:], "\n")
	heading := fmt.Sprintf("## %s (%s)", next, releaseDate)
	return strings.TrimRight(content[:headerEnd], "\n") + "\n\n" + heading + "\n\n" + after, nil
}

func releaseNotesForVersion(content, target string) (string, error) {
	if _, err := parseVersion(target); err != nil {
		return "", err
	}
	headingPattern := regexp.MustCompile(`(?m)^## ` + regexp.QuoteMeta(target) + `(?: \([^)]+\))?$`)
	match := headingPattern.FindStringIndex(content)
	if match == nil {
		return "", fmt.Errorf("release notes do not contain version %s", target)
	}
	rest := content[match[1]:]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		rest = rest[:next]
	}
	body := strings.TrimSpace(rest)
	if body == "" {
		return "", fmt.Errorf("release notes for %s are empty", target)
	}
	return body + "\n", nil
}

func readFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(content), nil
}

func writeFile(path, content string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(content), info.Mode()); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func prepare(args []string) error {
	flags := flag.NewFlagSet("prepare", flag.ContinueOnError)
	next := flags.String("version", "", "release version in X.Y.Z format")
	releaseDate := flags.String("date", time.Now().UTC().Format(time.DateOnly), "release date in YYYY-MM-DD format")
	packageFile := flags.String("package-file", "package.nix", "path to package.nix")
	notesFile := flags.String("notes-file", "release-notes.md", "path to release notes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *next == "" {
		return errors.New("--version is required")
	}

	packageContent, err := readFile(*packageFile)
	if err != nil {
		return err
	}
	notesContent, err := readFile(*notesFile)
	if err != nil {
		return err
	}
	updatedPackage, err := updatePackageVersion(packageContent, *next)
	if err != nil {
		return err
	}
	updatedNotes, err := updateReleaseNotes(notesContent, *next, *releaseDate)
	if err != nil {
		return err
	}
	if err := writeFile(*packageFile, updatedPackage); err != nil {
		return err
	}
	if err := writeFile(*notesFile, updatedNotes); err != nil {
		return err
	}
	fmt.Printf("prepared release %s (%s)\n", *next, *releaseDate)
	return nil
}

func printCurrentVersion(args []string) error {
	flags := flag.NewFlagSet("current-version", flag.ContinueOnError)
	packageFile := flags.String("package-file", "package.nix", "path to package.nix")
	if err := flags.Parse(args); err != nil {
		return err
	}
	content, err := readFile(*packageFile)
	if err != nil {
		return err
	}
	value, err := currentVersion(content)
	if err != nil {
		return err
	}
	fmt.Println(value)
	return nil
}

func printReleaseNotes(args []string) error {
	flags := flag.NewFlagSet("release-notes", flag.ContinueOnError)
	target := flags.String("version", "", "release version in X.Y.Z format")
	notesFile := flags.String("notes-file", "release-notes.md", "path to release notes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *target == "" {
		return errors.New("--version is required")
	}
	content, err := readFile(*notesFile)
	if err != nil {
		return err
	}
	body, err := releaseNotesForVersion(content, *target)
	if err != nil {
		return err
	}
	fmt.Print(body)
	return nil
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected command: prepare, current-version, or release-notes")
	}
	switch args[0] {
	case "prepare":
		return prepare(args[1:])
	case "current-version":
		return printCurrentVersion(args[1:])
	case "release-notes":
		return printReleaseNotes(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
