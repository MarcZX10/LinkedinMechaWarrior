package api

import (
	"context"
	"strconv"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/normalized"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

// Notification is one card of the notifications page.
type Notification struct {
	Text string `json:"text"`
	Date string `json:"date,omitempty"`
	URL  string `json:"url,omitempty"`
	Read bool   `json:"read"`
}

// The notifications page as the website reads it, then an older form.
var notificationEndpoints = []struct{ decoration, q string }{
	{"com.linkedin.voyager.dash.deco.identity.notifications.CardsCollectionWithInjectionsNoPills-25", "filterVanityName"},
	{"com.linkedin.voyager.dash.deco.identity.notifications.CardsCollection-80", "notifications"},
}

// Notifications returns your latest notifications. Reading them does not
// mark them as seen.
func Notifications(ctx context.Context, c Client, limit int) ([]Notification, error) {
	if err := checkLimit(limit, MaxListItems); err != nil {
		return nil, err
	}
	return paginate(limit, func(start int) ([]Notification, int, error) {
		var raw []byte
		var err error
		for _, e := range notificationEndpoints {
			raw, err = c.Get(ctx, "/voyagerIdentityDashNotificationCards", voyager.Params{
				{Key: "decorationId", Value: e.decoration},
				{Key: "count", Value: strconv.Itoa(ListPageSize)},
				{Key: "q", Value: e.q},
				{Key: "start", Value: strconv.Itoa(start)},
			})
			if !endpointGone(err) {
				break
			}
		}
		if err != nil {
			return nil, 0, err
		}
		found, next := ParseNotifications(raw, start)
		return found, next, nil
	})
}

// ParseNotifications reads a page of notification cards that started at
// start, and returns where the next page starts.
func ParseNotifications(raw []byte, start int) ([]Notification, int) {
	doc := normalized.Parse(raw)
	cards, n, coll := collection(doc)
	if coll == nil {
		cards = doc.OfType("Card")
		n = len(cards)
	}
	var out []Notification
	for _, card := range cards {
		text := firstNonEmpty(normalized.Text(card["headline"]), normalized.Text(card["contentPrimaryText"]))
		if text == "" {
			continue
		}
		read, _ := card["read"].(bool)
		out = append(out, Notification{
			Text: text,
			Date: millisDate(firstOf(card, "publishedAt", "timestamp")),
			URL: firstNonEmpty(normalized.String(normalized.Dig(card, "cardAction", "actionTarget")),
				normalized.String(normalized.Dig(card, "headerImage", "actionTarget"))),
			Read: read,
		})
	}
	return out, pageEnd(coll, start, n, ListPageSize)
}
