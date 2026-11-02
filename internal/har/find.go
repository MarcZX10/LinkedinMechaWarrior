package har

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Browsers don't keep HAR files: they save them like downloads. Finding the
// one you just recorded means looking where your browser saves things, so
// lmw can use it without you typing its path. Callers ask the user before
// looking, since this reads folders of theirs.

// Place is a folder where a HAR file may have been saved, or (Recent) the
// folder that holds the desktop's list of recently saved files.
type Place struct {
	Label  string `json:"label"`
	Dir    string `json:"dir"`
	Recent bool   `json:"recent,omitempty"`
}

// Found is a HAR file with LinkedIn traffic.
type Found struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// Env is what Places needs to know about the computer; tests replace it.
type Env struct {
	Home    string
	Getenv  func(string) string
	Browser string // the default browser: chrome, edge, firefox, brave...
	WorkDir string
}

// browsers are the browsers whose folders lmw knows how to read.
var browsers = []string{"firefox", "chrome", "edge", "brave", "vivaldi", "chromium"}

// Places returns where to look: the recent files, the download and last-save
// folders of the installed browsers (the default one first), then Downloads,
// Desktop and the current folder, without repeats or missing folders.
func Places(env Env) []Place {
	var places []Place
	if recent, ok := recentPlace(env); ok {
		places = append(places, recent)
	}
	order := []string{env.Browser}
	for _, b := range browsers {
		if b != env.Browser {
			order = append(order, b)
		}
	}
	for _, b := range order {
		places = append(places, browserFolders(env, b)...)
	}
	places = append(places, Place{Label: "Downloads", Dir: downloadsDir(env)})
	places = append(places, Place{Label: "Desktop", Dir: filepath.Join(env.Home, "Desktop")})
	if env.WorkDir != "" {
		places = append(places, Place{Label: "this folder", Dir: env.WorkDir})
	}
	var out []Place
	seen := map[string]bool{}
	for _, p := range places {
		if p.Dir == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(p.Dir))
		if p.Recent {
			key = "recent:" + key
		}
		if info, err := os.Stat(p.Dir); err != nil || !info.IsDir() || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

// sniffBytes is how much of each HAR file is read to tell whether it has
// LinkedIn traffic; request URLs come first in each entry.
const sniffBytes = 512 << 10

// Find returns the HAR files with LinkedIn traffic in places, newest first.
// It only reads the start of each .har file, doesn't look in subfolders, and
// for the recent files place reads the shortcuts of .har files.
func Find(places []Place) []Found {
	var paths []string
	for _, place := range places {
		if place.Recent {
			paths = append(paths, recentHARs(place)...)
			continue
		}
		entries, err := os.ReadDir(place.Dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".har") {
				paths = append(paths, filepath.Join(place.Dir, entry.Name()))
			}
		}
	}
	var found []Found
	seen := map[string]bool{}
	for _, path := range paths {
		key := strings.ToLower(filepath.Clean(path))
		if seen[key] {
			continue
		}
		seen[key] = true
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || !mentionsLinkedIn(path) {
			continue
		}
		found = append(found, Found{Path: path, Size: info.Size(), Modified: info.ModTime()})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Modified.After(found[j].Modified) })
	return found
}

func mentionsLinkedIn(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head, _ := io.ReadAll(io.LimitReader(f, sniffBytes))
	return bytes.Contains(head, []byte("linkedin.com/voyager/api/"))
}

// downloadsDir is the user's Downloads folder, as XDG names it on Linux.
func downloadsDir(env Env) string {
	if dir := xdgDownloadDir(env); dir != "" {
		return dir
	}
	return filepath.Join(env.Home, "Downloads")
}

var xdgDownload = regexp.MustCompile(`(?m)^XDG_DOWNLOAD_DIR="([^"]+)"`)

func xdgDownloadDir(env Env) string {
	config := env.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(env.Home, ".config")
	}
	raw, err := os.ReadFile(filepath.Join(config, "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	m := xdgDownload.FindSubmatch(raw)
	if m == nil {
		return ""
	}
	return strings.ReplaceAll(string(m[1]), "$HOME", env.Home)
}

// browserFolders returns the download folder a browser was set to use and
// the folder its save dialog last used, when its settings name them.
func browserFolders(env Env, browser string) []Place {
	if browser == "" {
		return nil
	}
	if browser == "firefox" {
		if dir := firefoxDownloadDir(env); dir != "" {
			return []Place{{Label: "firefox downloads", Dir: dir}}
		}
		return nil
	}
	var places []Place
	for _, prefs := range chromiumPreferences(env, browser) {
		raw, err := os.ReadFile(prefs)
		if err != nil {
			continue
		}
		var p struct {
			Download struct {
				DefaultDirectory string `json:"default_directory"`
			} `json:"download"`
			SaveFile struct {
				DefaultDirectory string `json:"default_directory"`
			} `json:"savefile"`
		}
		if json.Unmarshal(raw, &p) != nil {
			continue
		}
		if p.Download.DefaultDirectory != "" {
			places = append(places, Place{Label: browser + " downloads", Dir: p.Download.DefaultDirectory})
		}
		if p.SaveFile.DefaultDirectory != "" {
			places = append(places, Place{Label: browser + " last save", Dir: p.SaveFile.DefaultDirectory})
		}
	}
	return places
}

// chromiumPreferences lists the Preferences files of the default profile of
// a Chromium-based browser, on Windows and on Linux.
func chromiumPreferences(env Env, browser string) []string {
	windows := map[string][]string{
		"chrome": {"Google", "Chrome"}, "edge": {"Microsoft", "Edge"}, "brave": {"BraveSoftware", "Brave-Browser"},
		"vivaldi": {"Vivaldi"}, "chromium": {"Chromium"},
	}
	linux := map[string][]string{
		"chrome": {"google-chrome"}, "edge": {"microsoft-edge"}, "brave": {"BraveSoftware", "Brave-Browser"},
		"vivaldi": {"vivaldi"}, "chromium": {"chromium"},
	}
	var paths []string
	if local := env.Getenv("LOCALAPPDATA"); local != "" && windows[browser] != nil {
		parts := append(append([]string{local}, windows[browser]...), "User Data", "Default", "Preferences")
		paths = append(paths, filepath.Join(parts...))
	}
	if linux[browser] != nil {
		parts := append(append([]string{env.Home, ".config"}, linux[browser]...), "Default", "Preferences")
		paths = append(paths, filepath.Join(parts...))
	}
	return paths
}

var firefoxDownloadPref = regexp.MustCompile(`user_pref\("browser\.download\.dir",\s*"((?:[^"\\]|\\.)*)"\)`)

// firefoxDownloadDir reads browser.download.dir from the default profile.
func firefoxDownloadDir(env Env) string {
	var roots []string
	if appdata := env.Getenv("APPDATA"); appdata != "" {
		roots = append(roots, filepath.Join(appdata, "Mozilla", "Firefox"))
	}
	roots = append(roots, filepath.Join(env.Home, ".mozilla", "firefox"))
	for _, root := range roots {
		profile := firefoxDefaultProfile(root)
		if profile == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(profile, "prefs.js"))
		if err != nil {
			continue
		}
		if m := firefoxDownloadPref.FindSubmatch(raw); m != nil {
			var dir string
			if json.Unmarshal(append(append([]byte(`"`), m[1]...), '"'), &dir) == nil {
				return dir
			}
		}
	}
	return ""
}

// firefoxDefaultProfile finds the default profile folder in profiles.ini:
// the one an [Install...] section names, else the one marked Default=1,
// else the first one.
func firefoxDefaultProfile(root string) string {
	f, err := os.Open(filepath.Join(root, "profiles.ini"))
	if err != nil {
		return ""
	}
	defer f.Close()
	type section struct {
		name   string
		values map[string]string
	}
	var sections []section
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sections = append(sections, section{name: strings.Trim(line, "[]"), values: map[string]string{}})
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok && len(sections) > 0 {
			sections[len(sections)-1].values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	for _, sec := range sections {
		if strings.HasPrefix(sec.name, "Install") && sec.values["Default"] != "" {
			return filepath.Join(root, filepath.FromSlash(sec.values["Default"]))
		}
	}
	var first string
	for _, sec := range sections {
		path := sec.values["Path"]
		if !strings.HasPrefix(sec.name, "Profile") || path == "" {
			continue
		}
		if sec.values["IsRelative"] != "0" {
			path = filepath.Join(root, filepath.FromSlash(path))
		}
		if sec.values["Default"] == "1" {
			return path
		}
		if first == "" {
			first = path
		}
	}
	return first
}
