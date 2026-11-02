package har

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// DefaultBrowser names the browser Windows opens https links with, from the
// ProgId of the user's choice, or "" when it can't tell.
func DefaultBrowser() string {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\UserChoice`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	progID, _, err := key.GetStringValue("ProgId")
	if err != nil {
		return ""
	}
	return browserFromProgID(progID)
}

func browserFromProgID(progID string) string {
	id := strings.ToLower(progID)
	for prefix, browser := range map[string]string{
		"firefox": "firefox", "chrome": "chrome", "msedge": "edge", "brave": "brave",
		"vivaldi": "vivaldi", "chromium": "chromium", "opera": "opera",
	} {
		if strings.HasPrefix(id, prefix) {
			return browser
		}
	}
	return ""
}
