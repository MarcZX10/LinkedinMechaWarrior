package har

import (
	"encoding/binary"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"
)

// Saving a file from a browser's save dialog records it among the recent
// files of the desktop: a shortcut in Windows' Recent items, or an entry of
// recently-used.xbel on Linux. That finds a HAR wherever it was saved, with
// whichever browser.

// recentPlace is the place that holds the list of recent files, if any.
func recentPlace(env Env) (Place, bool) {
	if appdata := env.Getenv("APPDATA"); appdata != "" {
		return Place{Label: "recent files", Dir: filepath.Join(appdata, "Microsoft", "Windows", "Recent"), Recent: true}, true
	}
	data := env.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(env.Home, ".local", "share")
	}
	if _, err := os.Stat(filepath.Join(data, "recently-used.xbel")); err == nil {
		return Place{Label: "recent files", Dir: data, Recent: true}, true
	}
	return Place{}, false
}

// recentHARs lists the .har files a recent-files place points to.
func recentHARs(place Place) []string {
	var paths []string
	if raw, err := os.ReadFile(filepath.Join(place.Dir, "recently-used.xbel")); err == nil {
		for _, m := range xbelHref.FindAllStringSubmatch(string(raw), -1) {
			if u, err := url.Parse(m[1]); err == nil && u.Scheme == "file" {
				paths = append(paths, filepath.FromSlash(u.Path))
			}
		}
		return paths
	}
	entries, err := os.ReadDir(place.Dir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if !strings.HasSuffix(name, ".har.lnk") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(place.Dir, entry.Name()))
		if err != nil {
			continue
		}
		if target := shortcutTarget(raw); strings.EqualFold(filepath.Ext(target), ".har") {
			paths = append(paths, target)
		}
	}
	return paths
}

var xbelHref = regexp.MustCompile(`<bookmark[^>]*href="(file://[^"]+\.har)"`)

// shortcutTarget reads the local path a Windows shortcut (.lnk) points to,
// from its LinkInfo structure (MS-SHLLINK), or "" when it has none.
func shortcutTarget(raw []byte) string {
	u32 := func(at int) (int, bool) {
		if at < 0 || at+4 > len(raw) {
			return 0, false
		}
		return int(binary.LittleEndian.Uint32(raw[at:])), true
	}
	if size, ok := u32(0); !ok || size != 0x4C {
		return ""
	}
	flags, _ := u32(0x14)
	at := 0x4C
	if flags&0x1 != 0 { // HasLinkTargetIDList
		if at+2 > len(raw) {
			return ""
		}
		at += 2 + int(binary.LittleEndian.Uint16(raw[at:]))
	}
	if flags&0x2 == 0 { // HasLinkInfo
		return ""
	}
	info := at
	headerSize, ok1 := u32(info + 4)
	infoFlags, ok2 := u32(info + 8)
	if !ok1 || !ok2 || infoFlags&0x1 == 0 { // VolumeIDAndLocalBasePath
		return ""
	}
	if headerSize >= 0x24 {
		base, _ := u32(info + 0x1C)
		suffix, _ := u32(info + 0x20)
		if path := utf16z(raw, info+base) + utf16z(raw, info+suffix); path != "" {
			return path
		}
	}
	base, _ := u32(info + 0x10)
	suffix, _ := u32(info + 0x18)
	return ansiz(raw, info+base) + ansiz(raw, info+suffix)
}

func ansiz(raw []byte, at int) string {
	if at <= 0 || at >= len(raw) {
		return ""
	}
	end := at
	for end < len(raw) && raw[end] != 0 {
		end++
	}
	return string(raw[at:end])
}

func utf16z(raw []byte, at int) string {
	if at <= 0 || at >= len(raw) {
		return ""
	}
	var units []uint16
	for i := at; i+1 < len(raw); i += 2 {
		u := binary.LittleEndian.Uint16(raw[i:])
		if u == 0 {
			break
		}
		units = append(units, u)
	}
	return string(utf16.Decode(units))
}
