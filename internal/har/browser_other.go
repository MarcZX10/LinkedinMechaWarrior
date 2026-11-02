//go:build !windows

package har

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// DefaultBrowser names the default web browser on Linux desktops, from
// xdg-settings, or "" when it can't tell.
func DefaultBrowser() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xdg-settings", "get", "default-web-browser").Output()
	if err != nil {
		return ""
	}
	return browserFromDesktopFile(strings.TrimSpace(string(out)))
}

func browserFromDesktopFile(name string) string {
	name = strings.ToLower(name)
	for _, candidate := range []struct{ match, browser string }{
		{"firefox", "firefox"}, {"chrome", "chrome"}, {"edge", "edge"}, {"brave", "brave"},
		{"vivaldi", "vivaldi"}, {"chromium", "chromium"}, {"opera", "opera"},
	} {
		if strings.Contains(name, candidate.match) {
			return candidate.browser
		}
	}
	return ""
}
