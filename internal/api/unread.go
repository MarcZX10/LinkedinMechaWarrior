package api

import (
	"context"
	"strings"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/normalized"
)

// Unread counts what waits for you: the badges of LinkedIn's top bar and,
// when asked, the unread conversations of each message folder.
type Unread struct {
	Notifications int `json:"notifications"`
	Messages      int `json:"messages"`
	Network       int `json:"network"`
	// Other holds the badges lmw has no name for, as LinkedIn names them.
	Other map[string]int `json:"other,omitempty"`
	// Folders holds unread conversations by folder (see MessageFolders).
	Folders map[string]int `json:"folders,omitempty"`
}

var mailboxCountsQuery = Query{
	Name: "messengerMailboxCounts", Messaging: true, Vars: []string{"mailboxUrn"},
	Hashes: []string{"fc528a5a81a76dff212a4a3d2d48e84b"},
}

// UnreadCounts reads the badges of the top bar (one request). With folders
// it also counts unread conversations by folder (two more).
func UnreadCounts(ctx context.Context, c Client, folders bool) (Unread, error) {
	raw, err := c.Get(ctx, "/voyagerNotificationsDashBadgingItemCounts", nil)
	if err != nil {
		return Unread{}, err
	}
	unread := ParseBadges(raw)
	if !folders {
		return unread, nil
	}
	me, err := MyURN(ctx, c)
	if err != nil {
		return unread, err
	}
	raw, err = mailboxCountsQuery.Get(ctx, c, "(mailboxUrn:"+restliValue(me)+")")
	if err != nil {
		return unread, err
	}
	unread.Folders = ParseMailboxCounts(raw)
	return unread, nil
}

// ParseBadges reads the badge counts of the top bar.
func ParseBadges(raw []byte) Unread {
	var unread Unread
	items, _, _ := collection(normalized.Parse(raw))
	for _, item := range items {
		count, _ := normalized.Int(item["count"])
		switch badge := normalized.String(item["badgingItem"]); badge {
		case "NOTIFICATIONS":
			unread.Notifications = count
		case "MESSAGING":
			unread.Messages = count
		case "MY_NETWORK":
			unread.Network = count
		case "":
		default:
			if unread.Other == nil {
				unread.Other = map[string]int{}
			}
			unread.Other[strings.ToLower(badge)] = count
		}
	}
	return unread
}

// ParseMailboxCounts reads the unread conversations of each folder, named
// as lmw names folders.
func ParseMailboxCounts(raw []byte) map[string]int {
	names := map[string]string{}
	for name, category := range MessageFolders {
		names[category] = name
	}
	counts := map[string]int{}
	items, _, _ := collection(normalized.Parse(raw))
	for _, item := range items {
		if name, ok := names[normalized.String(item["category"])]; ok {
			counts[name], _ = normalized.Int(item["unreadConversationCount"])
		}
	}
	return counts
}
