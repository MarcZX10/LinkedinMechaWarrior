package har

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, path, content string, modified time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if !modified.IsZero() {
		_ = os.Chtimes(path, modified, modified)
	}
}

func TestPlacesReadTheBrowsersDownloadFolder(t *testing.T) {
	home := t.TempDir()
	saved := filepath.Join(home, "Saved HARs")
	_ = os.MkdirAll(saved, 0o700)
	_ = os.MkdirAll(filepath.Join(home, "Downloads"), 0o700)
	getenv := func(string) string { return "" }

	profiles := filepath.Join(home, ".mozilla", "firefox")
	write(t, filepath.Join(profiles, "profiles.ini"), "[Profile0]\nName=default\nIsRelative=1\nPath=abc.default\nDefault=1\n", time.Time{})
	escaped := filepath.ToSlash(saved)
	write(t, filepath.Join(profiles, "abc.default", "prefs.js"), `user_pref("browser.download.dir", "`+escaped+`");`+"\n", time.Time{})

	places := Places(Env{Home: home, Getenv: getenv, Browser: "firefox"})
	if len(places) != 2 || places[0].Label != "firefox downloads" || filepath.Clean(places[0].Dir) != filepath.Clean(saved) {
		t.Fatalf("firefox places %+v", places)
	}

	chrome := filepath.Join(home, ".config", "google-chrome", "Default", "Preferences")
	write(t, chrome, `{"download":{"default_directory":`+quote(saved)+`}}`, time.Time{})
	places = Places(Env{Home: home, Getenv: getenv, Browser: "chrome"})
	if places[0].Label != "chrome downloads" || filepath.Clean(places[0].Dir) != filepath.Clean(saved) {
		t.Errorf("chrome places %+v", places)
	}

	// Installed browsers count even when another is the default; the same
	// folder twice (Chrome's is Firefox's here, and this folder is Downloads),
	// and folders that don't exist, are listed once or not at all.
	places = Places(Env{Home: home, Getenv: getenv, Browser: "edge", WorkDir: filepath.Join(home, "Downloads")})
	if len(places) != 2 || places[0].Label != "firefox downloads" || places[1].Label != "Downloads" {
		t.Errorf("places with another default browser %+v", places)
	}
}

// shortcut builds a minimal Windows shortcut to target, with a LinkInfo
// structure holding its local path.
func shortcut(target string) []byte {
	put := func(b []byte, at, v int) { binary.LittleEndian.PutUint32(b[at:], uint32(v)) }
	header := make([]byte, 0x4C)
	put(header, 0, 0x4C)
	put(header, 0x14, 0x2) // HasLinkInfo
	volume := make([]byte, 16)
	put(volume, 0, 16)
	base := append([]byte(target), 0)
	info := make([]byte, 0x1C)
	volumeAt := len(info)
	baseAt := volumeAt + len(volume)
	suffixAt := baseAt + len(base)
	put(info, 0, suffixAt+1)
	put(info, 4, 0x1C)
	put(info, 8, 1) // VolumeIDAndLocalBasePath
	put(info, 0xC, volumeAt)
	put(info, 0x10, baseAt)
	put(info, 0x18, suffixAt)
	out := append(header, info...)
	out = append(out, volume...)
	out = append(out, base...)
	return append(out, 0)
}

func TestRecentFilesFindHARsSavedAnywhere(t *testing.T) {
	saved := filepath.Join(t.TempDir(), "elsewhere", "linkedin.har")
	write(t, saved, `{"log":{"entries":[{"request":{"url":"https://www.linkedin.com/voyager/api/me"}}]}}`, time.Time{})
	recent := t.TempDir()
	write(t, filepath.Join(recent, "linkedin.har.lnk"), string(shortcut(saved)), time.Time{})
	write(t, filepath.Join(recent, "notes.txt.lnk"), string(shortcut(filepath.Join(recent, "notes.txt"))), time.Time{})

	if got := shortcutTarget(shortcut(saved)); got != saved {
		t.Errorf("shortcut target %q, want %q", got, saved)
	}
	found := Find([]Place{{Label: "recent files", Dir: recent, Recent: true}})
	if len(found) != 1 || found[0].Path != saved {
		t.Errorf("found %+v", found)
	}

	xbel := t.TempDir()
	write(t, filepath.Join(xbel, "recently-used.xbel"),
		`<xbel><bookmark href="file:///home/ada/Saved%20HARs/linkedin.har" added="x"/><bookmark href="file:///home/ada/a.txt"/></xbel>`, time.Time{})
	paths := recentHARs(Place{Dir: xbel, Recent: true})
	if len(paths) != 1 || filepath.ToSlash(paths[0]) != "/home/ada/Saved HARs/linkedin.har" {
		t.Errorf("xbel paths %q", paths)
	}
	if shortcutTarget([]byte("not a shortcut")) != "" {
		t.Error("expected no target for garbage")
	}
}

func quote(s string) string {
	out := `"`
	for _, r := range s {
		if r == '\\' || r == '"' {
			out += `\`
		}
		out += string(r)
	}
	return out + `"`
}

func TestFindListsLinkedInHARsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	linkedin := `{"log":{"entries":[{"request":{"url":"https://www.linkedin.com/voyager/api/me"}}]}}`
	write(t, filepath.Join(dir, "old.har"), linkedin, now.Add(-time.Hour))
	write(t, filepath.Join(dir, "new.HAR"), linkedin, now)
	write(t, filepath.Join(dir, "other.har"), `{"log":{"entries":[{"request":{"url":"https://example.com/"}}]}}`, now)
	write(t, filepath.Join(dir, "notes.txt"), linkedin, now)
	write(t, filepath.Join(dir, "sub", "deep.har"), linkedin, now)

	found := Find([]Place{{Label: "test", Dir: dir}})
	if len(found) != 2 || filepath.Base(found[0].Path) != "new.HAR" || filepath.Base(found[1].Path) != "old.har" {
		t.Errorf("found %+v", found)
	}
}
