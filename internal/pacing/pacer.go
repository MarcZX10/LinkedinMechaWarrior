// Package pacing keeps LinkedIn API usage light and irregular.
//
// Account restrictions are mostly triggered by volume and by machine-like
// regularity, so every request goes through a Pacer that:
//
//   - waits a randomized delay since the previous request, also across
//     separate runs, because the log is persisted on disk;
//   - enforces hourly and daily request budgets;
//   - enforces a cooldown after LinkedIn pushes back (challenge, 429, 999),
//     so a session that is already under suspicion is left alone.
package pacing

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
)

const (
	hour = time.Hour
	day  = 24 * time.Hour
)

// Usage summarizes the request log.
type Usage struct {
	LastHour       int        `json:"last_hour"`
	LastDay        int        `json:"last_day"`
	HourlyBudget   int        `json:"hourly_budget"`
	DailyBudget    int        `json:"daily_budget"`
	CooldownUntil  *time.Time `json:"cooldown_until"`
	CooldownReason string     `json:"cooldown_reason,omitempty"`
}

type state struct {
	Requests       []float64 `json:"requests"`
	CooldownUntil  *float64  `json:"cooldown_until"`
	CooldownReason string    `json:"cooldown_reason,omitempty"`
}

// Pacer enforces delays, budgets and cooldowns. Clock, Sleep and Rand can be
// replaced in tests.
type Pacer struct {
	StateFile    string
	MinDelay     time.Duration
	MaxDelay     time.Duration
	HourlyBudget int
	DailyBudget  int

	Clock func() time.Time
	Sleep func(time.Duration)
	Rand  *rand.Rand
}

// New creates a Pacer with the real clock.
func New(stateFile string, minDelay, maxDelay time.Duration, hourlyBudget, dailyBudget int) *Pacer {
	return &Pacer{
		StateFile:    stateFile,
		MinDelay:     minDelay,
		MaxDelay:     maxDelay,
		HourlyBudget: hourlyBudget,
		DailyBudget:  dailyBudget,
		Clock:        time.Now,
		Sleep:        time.Sleep,
		Rand:         rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x6c6d77)),
	}
}

// BeforeRequest blocks until the next request is allowed, or returns an
// error if it is not allowed at all.
func (p *Pacer) BeforeRequest() error {
	st := p.load()
	now := p.Clock()

	if until := p.activeCooldown(st, now); until != nil {
		return errs.New(errs.Cooldown,
			"requests are paused until %s because LinkedIn pushed back: %s. "+
				"Check the account in your browser, then run `lmw limits --clear-cooldown` once you are sure it is fine",
			until.Format("15:04"), st.CooldownReason)
	}
	if len(st.Requests) >= p.DailyBudget {
		return errs.New(errs.Budget,
			"daily request budget reached (%d requests in 24h). Try again later or raise LMW_DAILY_REQUEST_BUDGET carefully",
			p.DailyBudget)
	}
	if countSince(st.Requests, now.Add(-hour)) >= p.HourlyBudget {
		return errs.New(errs.Budget,
			"hourly request budget reached (%d requests in 1h). Try again later or raise LMW_HOURLY_REQUEST_BUDGET carefully",
			p.HourlyBudget)
	}

	if n := len(st.Requests); n > 0 {
		last := fromUnix(st.Requests[n-1])
		if wait := p.nextDelay() - now.Sub(last); wait > 0 {
			slog.Debug("pacing before the next request", "wait", wait.Round(100*time.Millisecond))
			p.Sleep(wait)
		}
	}
	return nil
}

// RecordRequest appends the current time to the request log.
func (p *Pacer) RecordRequest() error {
	st := p.load()
	st.Requests = append(st.Requests, toUnix(p.Clock()))
	return p.save(st)
}

// StartCooldown pauses all requests for the given duration.
func (p *Pacer) StartCooldown(duration time.Duration, reason string) error {
	st := p.load()
	until := toUnix(p.Clock().Add(duration))
	st.CooldownUntil = &until
	st.CooldownReason = reason
	return p.save(st)
}

// ClearCooldown lifts an active cooldown.
func (p *Pacer) ClearCooldown() error {
	st := p.load()
	st.CooldownUntil = nil
	st.CooldownReason = ""
	return p.save(st)
}

// Usage reports the request counts and any active cooldown.
func (p *Pacer) Usage() Usage {
	st := p.load()
	now := p.Clock()
	usage := Usage{
		LastHour:     countSince(st.Requests, now.Add(-hour)),
		LastDay:      len(st.Requests),
		HourlyBudget: p.HourlyBudget,
		DailyBudget:  p.DailyBudget,
	}
	if until := p.activeCooldown(st, now); until != nil {
		usage.CooldownUntil = until
		usage.CooldownReason = st.CooldownReason
	}
	return usage
}

func (p *Pacer) activeCooldown(st state, now time.Time) *time.Time {
	if st.CooldownUntil == nil {
		return nil
	}
	until := fromUnix(*st.CooldownUntil)
	if !now.Before(until) {
		return nil
	}
	return &until
}

func (p *Pacer) nextDelay() time.Duration {
	span := p.MaxDelay - p.MinDelay
	delay := p.MinDelay + time.Duration(p.Rand.Float64()*float64(span))
	// Occasionally take a longer pause, like a person stopping to read something.
	if p.Rand.Float64() < 0.1 {
		delay += p.MaxDelay + time.Duration(p.Rand.Float64()*float64(2*p.MaxDelay))
	}
	return delay
}

func (p *Pacer) load() state {
	raw, err := os.ReadFile(p.StateFile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("request log is unreadable; starting a new one", "file", p.StateFile, "error", err)
		}
		return state{}
	}
	var st state
	if err := json.Unmarshal(raw, &st); err != nil {
		slog.Warn("request log is corrupted; starting a new one", "file", p.StateFile)
		return state{}
	}
	cutoff := toUnix(p.Clock().Add(-day))
	kept := st.Requests[:0]
	for _, ts := range st.Requests {
		if ts > cutoff {
			kept = append(kept, ts)
		}
	}
	sort.Float64s(kept)
	st.Requests = kept
	return st
}

func (p *Pacer) save(st state) error {
	if st.Requests == nil {
		st.Requests = []float64{}
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.StateFile), 0o700); err != nil {
		return errs.Wrap(errs.Storage, err, "cannot create %s", filepath.Dir(p.StateFile))
	}
	tmp := p.StateFile + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return errs.Wrap(errs.Storage, err, "cannot write the request log")
	}
	if err := os.Rename(tmp, p.StateFile); err != nil {
		return errs.Wrap(errs.Storage, err, "cannot write the request log")
	}
	return nil
}

func countSince(requests []float64, since time.Time) int {
	cutoff := toUnix(since)
	count := 0
	for _, ts := range requests {
		if ts > cutoff {
			count++
		}
	}
	return count
}

func toUnix(t time.Time) float64 { return float64(t.UnixNano()) / float64(time.Second) }

func fromUnix(ts float64) time.Time {
	return time.Unix(0, int64(ts*float64(time.Second)))
}

// String renders the usage for humans.
func (u Usage) String() string {
	text := fmt.Sprintf("Requests in the last hour: %d/%d\nRequests in the last 24h:  %d/%d\n",
		u.LastHour, u.HourlyBudget, u.LastDay, u.DailyBudget)
	if u.CooldownUntil != nil {
		return text + fmt.Sprintf("Cooldown active until %s: %s", u.CooldownUntil.Format("2006-01-02 15:04"), u.CooldownReason)
	}
	return text + "No cooldown active"
}
