package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func expandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func commandPath(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil && path != "" {
		return path, nil
	}
	user := envDefault("USER", "ko")
	candidates := []string{
		filepath.Join(os.Getenv("HOME"), ".nix-profile", "bin", name),
		filepath.Join("/etc/profiles/per-user", user, "bin", name),
		filepath.Join("/nix/var/nix/profiles/default/bin", name),
		filepath.Join("/usr/local/bin", name),
		filepath.Join("/usr/bin", name),
		filepath.Join("/bin", name),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s is required", name)
}

func homeShorten(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}

func envDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func fitText(text string, width int) string {
	text = truncate(text, width)
	for lipgloss.Width(text) > width {
		runes := []rune(text)
		if len(runes) == 0 {
			break
		}
		text = string(runes[:len(runes)-1])
	}
	return padRendered(text, width)
}

func padRendered(text string, width int) string {
	cellWidth := lipgloss.Width(text)
	if cellWidth >= width {
		return text
	}
	return text + strings.Repeat(" ", width-cellWidth)
}

func wrapLines(text string, width int) []string {
	width = max(1, width)
	var out []string
	for _, line := range strings.Split(text, "\n") {
		runes := []rune(line)
		if len(runes) == 0 {
			out = append(out, "")
			continue
		}
		for len(runes) > width {
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
		out = append(out, string(runes))
	}
	return out
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !shellSafeRune(r)
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func shellSafeRune(r rune) bool {
	return r == '_' || r == '-' || r == '/' || r == '.' || r == ':' || r == '@' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
