package pacing

import (
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
)

type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
}

func newPacer(dir string, clock *fakeClock, hourly, daily int) *Pacer {
	return &Pacer{
		StateFile:    filepath.Join(dir, "request-log.json"),
		MinDelay:     2 * time.Second,
		MaxDelay:     6 * time.Second,
		HourlyBudget: hourly,
		DailyBudget:  daily,
		Clock:        clock.Now,
		Sleep:        clock.Sleep,
		Rand:         rand.New(rand.NewPCG(42, 42)),
	}
}

func send(t *testing.T, p *Pacer) {
	t.Helper()
	if err := p.BeforeRequest(); err != nil {
		t.Fatal(err)
	}
	if err := p.RecordRequest(); err != nil {
		t.Fatal(err)
	}
}

func kindOf(err error) errs.Kind {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return 0
}

func start() *fakeClock { return &fakeClock{now: time.Unix(1_000_000, 0)} }

func TestFirstRequestDoesNotWait(t *testing.T) {
	clock := start()
	send(t, newPacer(t.TempDir(), clock, 60, 300))
	if len(clock.sleeps) != 0 {
		t.Errorf("unexpected sleeps %v", clock.sleeps)
	}
}

func TestConsecutiveRequestsWaitARandomizedDelay(t *testing.T) {
	clock := start()
	p := newPacer(t.TempDir(), clock, 60, 300)
	for range 20 {
		send(t, p)
	}
	if len(clock.sleeps) != 19 {
		t.Fatalf("expected 19 sleeps, got %d", len(clock.sleeps))
	}
	distinct := map[time.Duration]bool{}
	for _, d := range clock.sleeps {
		if d < 2*time.Second {
			t.Errorf("delay %v is shorter than the minimum", d)
		}
		distinct[d.Round(time.Millisecond)] = true
	}
	if len(distinct) < 10 {
		t.Errorf("delays look like a fixed cadence: %v", clock.sleeps)
	}
}

func TestSpacingIsKeptAcrossSeparatePacers(t *testing.T) {
	clock := start()
	dir := t.TempDir()
	send(t, newPacer(dir, clock, 60, 300))
	clock.now = clock.now.Add(time.Second)
	send(t, newPacer(dir, clock, 60, 300))
	if len(clock.sleeps) != 1 || clock.sleeps[0] < time.Second {
		t.Errorf("expected a wait carried over from the previous run, got %v", clock.sleeps)
	}
}

func TestNoWaitWhenEnoughTimeHasPassed(t *testing.T) {
	clock := start()
	p := newPacer(t.TempDir(), clock, 60, 300)
	send(t, p)
	clock.now = clock.now.Add(10 * time.Minute)
	send(t, p)
	if len(clock.sleeps) != 0 {
		t.Errorf("unexpected sleeps %v", clock.sleeps)
	}
}

func TestHourlyBudgetIsEnforcedAndRecovers(t *testing.T) {
	clock := start()
	p := newPacer(t.TempDir(), clock, 3, 300)
	for range 3 {
		send(t, p)
	}
	if err := p.BeforeRequest(); kindOf(err) != errs.Budget {
		t.Fatalf("expected a budget error, got %v", err)
	}
	clock.now = clock.now.Add(time.Hour)
	send(t, p)
}

func TestDailyBudgetIsEnforced(t *testing.T) {
	clock := start()
	p := newPacer(t.TempDir(), clock, 2, 3)
	for range 3 {
		send(t, p)
		clock.now = clock.now.Add(time.Hour)
	}
	if err := p.BeforeRequest(); kindOf(err) != errs.Budget {
		t.Fatalf("expected a budget error, got %v", err)
	}
	clock.now = clock.now.Add(24 * time.Hour)
	send(t, p)
}

func TestCooldownBlocksUntilExpiredOrCleared(t *testing.T) {
	clock := start()
	dir := t.TempDir()
	p := newPacer(dir, clock, 60, 300)
	if err := p.StartCooldown(time.Hour, "HTTP 429"); err != nil {
		t.Fatal(err)
	}
	if err := newPacer(dir, clock, 60, 300).BeforeRequest(); kindOf(err) != errs.Cooldown {
		t.Fatalf("expected a cooldown error, got %v", err)
	}
	if usage := p.Usage(); usage.CooldownReason != "HTTP 429" || usage.CooldownUntil == nil {
		t.Errorf("unexpected usage %+v", usage)
	}

	clock.now = clock.now.Add(time.Hour + time.Second)
	if err := p.BeforeRequest(); err != nil {
		t.Fatal(err)
	}
	if p.Usage().CooldownUntil != nil {
		t.Error("expired cooldown still reported")
	}

	_ = p.StartCooldown(time.Hour, "challenge")
	_ = p.ClearCooldown()
	if err := p.BeforeRequest(); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptedLogIsIgnored(t *testing.T) {
	clock := start()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "request-log.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := newPacer(dir, clock, 60, 300)
	send(t, p)
	if p.Usage().LastDay != 1 {
		t.Errorf("unexpected usage %+v", p.Usage())
	}
}
