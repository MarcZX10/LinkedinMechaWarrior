// Package watch keeps the things a person asked lmw to keep an eye on (the
// next post of someone, new jobs for a search, new messages...) and checks
// them: each check reads LinkedIn once or twice and returns what is new
// since the previous one.
package watch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/notify"
)

// MinMinutes is the shortest interval between two checks of a watch.
const MinMinutes = 15

// Watch is one thing to keep an eye on.
type Watch struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
	// Params holds what a kind needs besides the target, such as the member
	// id of a profile, found once when the watch is added.
	Params  map[string]string `json:"params,omitempty"`
	Minutes int               `json:"every_minutes"`
	// Once watches end after their first notification ("the next post").
	Once bool `json:"once,omitempty"`
	// Channel is where notifications go; empty means every channel.
	Channel string `json:"channel,omitempty"`
	// Note says why the watch exists, in the words of whoever added it.
	Note      string    `json:"note,omitempty"`
	Created   time.Time `json:"created"`
	LastRun   time.Time `json:"last_run"`
	LastError string    `json:"last_error,omitempty"`
	// Found counts what the watch told about so far.
	Found int `json:"found"`
	// Seen holds the keys of what the watch already saw; the first check
	// only fills it, so a watch tells about what comes after it was added.
	Seeded bool     `json:"seeded"`
	Seen   []string `json:"seen,omitempty"`
}

// Every is the interval between two checks.
func (w Watch) Every() time.Duration { return time.Duration(w.Minutes) * time.Minute }

// Due reports whether w should be checked at now.
func (w Watch) Due(now time.Time) bool { return !now.Before(w.LastRun.Add(w.Every())) }

// Describe says in a few words what w watches.
func (w Watch) Describe() string {
	k, ok := Find(w.Kind)
	if !ok {
		return w.Kind
	}
	if w.Target == "" {
		return k.About
	}
	return k.About + ": " + w.Target
}

// New makes a watch of kind with an id, checked every minutes.
func New(kind, target string, minutes int) (Watch, error) {
	k, ok := Find(kind)
	if !ok {
		return Watch{}, fmt.Errorf("unknown kind of watch %q; use %s", kind, KindNames())
	}
	if k.Target != "" && target == "" {
		return Watch{}, fmt.Errorf("a %s watch needs %s", kind, k.Target)
	}
	if k.Target == "" && target != "" {
		return Watch{}, fmt.Errorf("a %s watch takes no target", kind)
	}
	if minutes < MinMinutes {
		return Watch{}, fmt.Errorf("check every %d minutes or more, so watches leave requests for the rest of lmw", MinMinutes)
	}
	id := make([]byte, 3)
	_, _ = rand.Read(id)
	return Watch{ID: hex.EncodeToString(id), Kind: kind, Target: target, Minutes: minutes, Params: map[string]string{}, Created: time.Now().UTC()}, nil
}

// PerHour is how many requests an hour the watches use.
func PerHour(watches []Watch) float64 {
	total := 0.0
	for _, w := range watches {
		if k, ok := Find(w.Kind); ok && w.Minutes > 0 {
			total += float64(k.Cost) * 60 / float64(w.Minutes)
		}
	}
	return total
}

// Prepare checks the target of w and finds what later checks need, so each
// check costs as little as possible. It may read LinkedIn once.
func Prepare(ctx context.Context, c api.Client, w *Watch) error {
	k, _ := Find(w.Kind)
	if k.Prepare == nil {
		return nil
	}
	return k.Prepare(ctx, c, w)
}

// keep is how many keys a watch remembers.
const keep = 300

// Check reads LinkedIn for w and returns the messages to send about what is
// new. It updates w: when it ran, what it saw and the last error.
func Check(ctx context.Context, c api.Client, w *Watch, now time.Time) ([]notify.Message, error) {
	k, ok := Find(w.Kind)
	if !ok {
		return nil, fmt.Errorf("unknown kind of watch %q", w.Kind)
	}
	w.LastRun = now.UTC()
	items, err := k.Check(ctx, c, w)
	if err != nil {
		w.LastError = err.Error()
		return nil, err
	}
	w.LastError = ""
	fresh := k.fresh(w.Seen, items)
	first := !w.Seeded
	w.Seeded = true
	if k.Replace {
		w.Seen = keysOf(items)
	} else {
		w.Seen = remember(w.Seen, items)
	}
	if first || len(fresh) == 0 {
		return nil, nil
	}
	w.Found += len(fresh)
	var messages []notify.Message
	for i, item := range fresh {
		if i == 4 && len(fresh) > 5 {
			messages = append(messages, notify.Message{Title: fmt.Sprintf("…and %d more (%s)", len(fresh)-4, w.Describe()), Watch: w.ID})
			break
		}
		messages = append(messages, notify.Message{Title: item.Title, Text: item.Text, URL: item.URL, Watch: w.ID})
	}
	return messages, nil
}

func keysOf(items []Item) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	return keys
}

// remember adds the keys of items to seen, keeping the newest.
func remember(seen []string, items []Item) []string {
	known := map[string]bool{}
	for _, key := range seen {
		known[key] = true
	}
	for _, item := range items {
		if !known[item.Key] {
			seen = append(seen, item.Key)
			known[item.Key] = true
		}
	}
	if len(seen) > keep {
		seen = seen[len(seen)-keep:]
	}
	return seen
}

// Store keeps the watches in a JSON file. The daemon and the commands that
// add or remove watches may run at once, so changes take a lock.
type Store struct{ Path string }

// Load returns the watches, none if there is no file yet.
func (s Store) Load() ([]Watch, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Watches []Watch `json:"watches"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("%s is not valid: %w", s.Path, err)
	}
	return file.Watches, nil
}

// Update changes the watches under the lock.
func (s Store) Update(change func([]Watch) ([]Watch, error)) error {
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	watches, err := s.Load()
	if err != nil {
		return err
	}
	watches, err = change(watches)
	if err != nil {
		return err
	}
	sort.SliceStable(watches, func(i, j int) bool { return watches[i].Created.Before(watches[j].Created) })
	raw, err := json.MarshalIndent(map[string]any{"watches": watches}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// lock takes the lock file, waiting a little if someone else has it. A lock
// older than lockStale was left by a process that died, and is taken over.
func (s Store) lock() (func(), error) {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return nil, err
	}
	path := s.Path + ".lock"
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > lockStale {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the watches are busy (%s); try again", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

const (
	lockWait  = 10 * time.Second
	lockStale = 30 * time.Second
)
