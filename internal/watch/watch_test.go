package watch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

// badges answers the unread badges with the counts it holds.
type badges struct{ notifications, messages, network int }

func (b *badges) Get(_ context.Context, path string, _ voyager.Params) ([]byte, error) {
	if path != "/voyagerNotificationsDashBadgingItemCounts" {
		return nil, fmt.Errorf("unexpected %s", path)
	}
	return []byte(fmt.Sprintf(`{"data":{"elements":[{"count":%d,"badgingItem":"NOTIFICATIONS"},{"count":%d,"badgingItem":"MESSAGING"},{"count":%d,"badgingItem":"MY_NETWORK"}]},"included":[]}`,
		b.notifications, b.messages, b.network)), nil
}
func (b *badges) Post(context.Context, string, voyager.Params, any) ([]byte, error) {
	return nil, errors.New("no writes")
}
func (b *badges) Delete(context.Context, string, voyager.Params) ([]byte, error) {
	return nil, errors.New("no writes")
}
func (b *badges) PostSDUI(context.Context, string, voyager.Params, any) ([]byte, error) {
	return nil, errors.New("no writes")
}
func (b *badges) Upload(context.Context, string, string, map[string]string, []byte) error {
	return errors.New("no writes")
}

// stream is a kind whose checks see the items a test gives it.
func stream(t *testing.T, items *[]Item) {
	t.Helper()
	Kinds = append(Kinds, Kind{Name: "stream", About: "a test stream", Cost: 1,
		Check: func(context.Context, api.Client, *Watch) ([]Item, error) { return *items, nil }})
	t.Cleanup(func() { Kinds = Kinds[:len(Kinds)-1] })
}

func TestTheFirstCheckOnlyRemembers(t *testing.T) {
	items := []Item{{Key: "1", Title: "old"}}
	stream(t, &items)
	w, err := New("stream", "", 30)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if msgs, err := Check(context.Background(), nil, &w, now); err != nil || len(msgs) != 0 || !w.Seeded {
		t.Fatalf("first check %v, %v, %+v", msgs, err, w)
	}
	if w.Due(now.Add(29*time.Minute)) || !w.Due(now.Add(30*time.Minute)) {
		t.Errorf("due times wrong for %+v", w)
	}
	items = append(items, Item{Key: "2", Title: "New post", Text: "hello", URL: "https://www.linkedin.com/x"})
	msgs, err := Check(context.Background(), nil, &w, now)
	if err != nil || len(msgs) != 1 || msgs[0].Title != "New post" || msgs[0].Watch != w.ID || w.Found != 1 {
		t.Fatalf("second check %+v, %v", msgs, err)
	}
	if msgs, _ := Check(context.Background(), nil, &w, now); len(msgs) != 0 {
		t.Errorf("told twice: %+v", msgs)
	}
}

func TestManyNewItemsAreSummed(t *testing.T) {
	var items []Item
	stream(t, &items)
	w, _ := New("stream", "", 30)
	_, _ = Check(context.Background(), nil, &w, time.Now())
	for i := range 8 {
		items = append(items, Item{Key: fmt.Sprint(i), Title: fmt.Sprint("item ", i)})
	}
	msgs, _ := Check(context.Background(), nil, &w, time.Now())
	if len(msgs) != 5 || !strings.HasPrefix(msgs[4].Title, "…and 4 more") || w.Found != 8 {
		t.Errorf("%d messages, last %+v", len(msgs), msgs[len(msgs)-1])
	}
}

func TestUnreadTellsOnlyWhatWentUp(t *testing.T) {
	b := &badges{notifications: 2, messages: 1}
	w, _ := New("unread", "", 15)
	if msgs, err := Check(context.Background(), b, &w, time.Now()); err != nil || len(msgs) != 0 {
		t.Fatalf("seed %v, %v", msgs, err)
	}
	b.notifications, b.messages = 1, 3
	msgs, err := Check(context.Background(), b, &w, time.Now())
	if err != nil || len(msgs) != 1 || msgs[0].Title != "3 unread messages" {
		t.Fatalf("went up %+v, %v", msgs, err)
	}
	b.notifications = 2
	if msgs, _ := Check(context.Background(), b, &w, time.Now()); len(msgs) != 1 || msgs[0].Title != "2 unread notifications" {
		t.Errorf("back up after going down %+v", msgs)
	}
}

func TestNewChecksItsInput(t *testing.T) {
	for _, c := range []struct {
		kind, target string
		minutes      int
		want         string
	}{
		{"tweets", "", 30, "unknown kind"},
		{"posts", "", 30, "needs a profile"},
		{"unread", "x", 30, "takes no target"},
		{"unread", "", 5, "15 minutes or more"},
	} {
		if _, err := New(c.kind, c.target, c.minutes); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

func TestPreparingAProfileWatchKeepsTheMemberID(t *testing.T) {
	w, _ := New("posts", "urn:li:fsd_profile:ACoAABcdefghijklmnopqrstu", 30)
	if err := Prepare(context.Background(), nil, &w); err != nil {
		t.Fatal(err)
	}
	if w.Params["member_id"] != "ACoAABcdefghijklmnopqrstu" || profileOf(&w).MemberID != "ACoAABcdefghijklmnopqrstu" {
		t.Errorf("%+v", w)
	}
}

func TestPerHour(t *testing.T) {
	a, _ := New("messages", "", 30) // 2 requests, twice an hour
	b, _ := New("unread", "", 15)   // 1 request, four times an hour
	if got := PerHour([]Watch{a, b}); got != 8 {
		t.Errorf("PerHour = %v", got)
	}
}

func TestStoreUpdatesDoNotLoseChanges(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "watches.json")}
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, _ := New("unread", "", 30)
			if err := s.Update(func(ws []Watch) ([]Watch, error) { return append(ws, w), nil }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	watches, err := s.Load()
	if err != nil || len(watches) != 10 {
		t.Fatalf("%d watches, %v", len(watches), err)
	}
}
