package api

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/normalized"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

const (
	// MaxListItems caps how many invitations, connections, notifications or
	// search results one command may fetch.
	MaxListItems = 50
	// ListPageSize is the page size used for those lists.
	ListPageSize = 10
	// MaxInvitationNote is LinkedIn's limit for the note of an invitation.
	MaxInvitationNote = 300

	// ConnectionsDecoration selects connections with their profiles.
	ConnectionsDecoration = "com.linkedin.voyager.dash.deco.web.mynetwork.ConnectionListWithProfile-16"
)

// Invitation is a connection request, received or sent.
type Invitation struct {
	ID       string `json:"id"`
	Secret   string `json:"-"`
	Name     string `json:"name,omitempty"`
	Headline string `json:"headline,omitempty"`
	Profile  string `json:"profile_url,omitempty"`
	Message  string `json:"message,omitempty"`
	Sent     string `json:"sent,omitempty"`
}

// Connection is one of your 1st-degree connections.
type Connection struct {
	Name     string `json:"name"`
	Headline string `json:"headline,omitempty"`
	URL      string `json:"url,omitempty"`
	URN      string `json:"urn,omitempty"`
	Since    string `json:"since,omitempty"`
}

// checkLimit validates the "how many" argument of list commands.
func checkLimit(limit, max int) error {
	if limit < 1 || limit > max {
		return errs.New(errs.Usage, "--limit must be between 1 and %d", max)
	}
	return nil
}

// paginate fetches pages until it has limit items. page returns the items
// of the page starting at start and where the next page starts, or a next
// start that is not after start when there are no more pages.
func paginate[T any](limit int, page func(start int) ([]T, int, error)) ([]T, error) {
	var items []T
	start := 0
	// Hard cap on pages, in case pages keep coming back with elements we can't parse.
	maxPages := (limit+ListPageSize-1)/ListPageSize + 1
	for range maxPages {
		found, next, err := page(start)
		if err != nil {
			return nil, err
		}
		items = append(items, found...)
		if next <= start || len(items) >= limit {
			break
		}
		start = next
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// pageEnd is where the page after one of n elements, asked with this page
// size, starts: LinkedIn's nextStart when it gives one, otherwise start+n,
// or start (no more pages) after an empty or short page.
func pageEnd(coll normalized.Object, start, n, size int) int {
	if n == 0 {
		return start
	}
	if next := nextStart(coll, start, n); next != start+n || n >= size {
		return next
	}
	return start
}

// personIn finds the member an entity is about: an inline or referenced
// profile, under the first of keys that has one.
func personIn(doc *normalized.Doc, entity normalized.Object, keys ...string) normalized.Object {
	for _, key := range keys {
		if person := doc.Ref(entity, key); person != nil {
			return person
		}
		if urn, ok := entity[key].(string); ok {
			if person := doc.Get(urn); person != nil {
				return person
			}
		}
	}
	return nil
}

func personSummary(person normalized.Object) (name, headline, url, urn string) {
	name = strings.TrimSpace(normalized.String(person["firstName"]) + " " + normalized.String(person["lastName"]))
	headline = firstNonEmpty(normalized.Text(person["occupation"]), normalized.Text(person["headline"]))
	if public := normalized.String(person["publicIdentifier"]); public != "" {
		url = "https://www.linkedin.com/in/" + public + "/"
	}
	urn = firstNonEmpty(normalized.String(person["dashEntityUrn"]), normalized.String(person["entityUrn"]))
	if id := memberIDFromURN(urn); id != "" {
		urn = "urn:li:fsd_profile:" + id
	}
	return
}

// Invitations returns received connection requests, newest first.
func Invitations(ctx context.Context, c Client, limit int) ([]Invitation, error) {
	if err := checkLimit(limit, MaxListItems); err != nil {
		return nil, err
	}
	return paginate(limit, func(start int) ([]Invitation, int, error) { return invitationPage(ctx, c, start) })
}

func invitationPage(ctx context.Context, c Client, start int) ([]Invitation, int, error) {
	raw, err := c.Get(ctx, "/relationships/invitationViews", voyager.Params{
		{Key: "start", Value: strconv.Itoa(start)},
		{Key: "count", Value: strconv.Itoa(ListPageSize)},
		{Key: "includeInsights", Value: "true"},
		{Key: "q", Value: "receivedInvitation"},
	})
	if err != nil {
		return nil, 0, err
	}
	found, next := ParseInvitations(raw, "fromMember", start)
	return found, next, nil
}

// SentInvitations returns the connection requests you sent that are pending.
func SentInvitations(ctx context.Context, c Client, limit int) ([]Invitation, error) {
	if err := checkLimit(limit, MaxListItems); err != nil {
		return nil, err
	}
	return paginate(limit, func(start int) ([]Invitation, int, error) {
		raw, err := c.Get(ctx, "/relationships/sentInvitationViewsV2", voyager.Params{
			{Key: "count", Value: strconv.Itoa(ListPageSize)},
			{Key: "invitationType", Value: "CONNECTION"},
			{Key: "q", Value: "invitationType"},
			{Key: "start", Value: strconv.Itoa(start)},
		})
		if err != nil {
			return nil, 0, err
		}
		found, next := ParseInvitations(raw, "toMember", start)
		return found, next, nil
	})
}

// ParseInvitations reads a page of invitations that started at start; who
// names the member the invitations are from ("fromMember") or to
// ("toMember"). It also returns where the next page starts.
func ParseInvitations(raw []byte, who string, start int) ([]Invitation, int) {
	doc := normalized.Parse(raw)
	views, n, coll := collection(doc)
	var invitations []normalized.Object
	for _, view := range views {
		if inv := doc.Ref(view, "invitation"); inv != nil {
			invitations = append(invitations, inv)
		} else if normalized.String(view["sharedSecret"]) != "" || view["*"+who] != nil {
			invitations = append(invitations, view)
		}
	}
	if coll == nil {
		invitations = doc.OfType(".Invitation")
		n = len(invitations)
	}
	var out []Invitation
	for _, inv := range invitations {
		id := invitationID(firstNonEmpty(normalized.String(inv["entityUrn"]), normalized.String(inv["invitationUrn"])))
		if id == "" {
			continue
		}
		person := personIn(doc, inv, who, who+"ResolutionResult", "genericInviter", "invitee", "inviter")
		name, headline, url, _ := personSummary(person)
		out = append(out, Invitation{
			ID: id, Secret: normalized.String(inv["sharedSecret"]),
			Name: name, Headline: headline, Profile: url,
			Message: normalized.Text(inv["message"]),
			Sent:    millisDate(inv["sentTime"]),
		})
	}
	return out, pageEnd(coll, start, n, ListPageSize)
}

// invitationID returns the number at the end of an invitation URN, such as
// urn:li:fs_relInvitation:7123 or urn:li:fsd_invitation:7123.
func invitationID(urn string) string {
	urn = strings.TrimSuffix(urn, ")")
	i := strings.LastIndexAny(urn, ":,")
	if i < 0 || i == len(urn)-1 {
		return ""
	}
	id := urn[i+1:]
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return ""
	}
	return id
}

func millisDate(value any) string {
	ms, ok := normalized.Int(value)
	if !ok || ms <= 0 {
		return ""
	}
	return time.UnixMilli(int64(ms)).UTC().Format("2006-01-02")
}

// InvitationAction is what to do with a received invitation.
type InvitationAction string

// The answers to a received invitation.
const (
	Accept InvitationAction = "accept"
	Ignore InvitationAction = "ignore"
)

// FindInvitation returns the received invitation with this id. Answering an
// invitation needs its shared secret, which only the list has, so this reads
// pages of the list until it finds it.
func FindInvitation(ctx context.Context, c Client, id string) (Invitation, error) {
	for start := 0; start < MaxListItems; {
		found, next, err := invitationPage(ctx, c, start)
		if err != nil {
			return Invitation{}, err
		}
		for _, inv := range found {
			if inv.ID == id {
				return inv, nil
			}
		}
		if next <= start {
			break
		}
		start = next
	}
	return Invitation{}, errs.New(errs.Usage, "no received invitation with id %s; list them with `lmw invitations`", id)
}

// RespondToInvitation accepts or ignores a received invitation. The caller
// is responsible for getting the user's confirmation first.
func RespondToInvitation(ctx context.Context, c Client, inv Invitation, action InvitationAction) error {
	_, err := c.Post(ctx, "/relationships/invitations/"+inv.ID, voyager.Params{{Key: "action", Value: string(action)}}, map[string]any{
		"invitationId":           inv.ID,
		"invitationSharedSecret": inv.Secret,
		"isGenericInvitation":    false,
	})
	return err
}

// WithdrawInvitation withdraws an invitation you sent.
func WithdrawInvitation(ctx context.Context, c Client, id string) error {
	if invitationID("urn:li:fsd_invitation:"+id) == "" {
		return errs.New(errs.Usage, "%q is not an invitation id; list them with `lmw invitations sent`", id)
	}
	path := "/voyagerRelationshipsDashInvitations/" + url.PathEscape("urn:li:fsd_invitation:"+id)
	_, err := c.Post(ctx, path, voyager.Params{{Key: "action", Value: "withdraw"}}, map[string]any{
		"sharedSecret":   nil,
		"invitationType": "CONNECTION",
	})
	return err
}

// Connect sends a connection request with an optional note.
func Connect(ctx context.Context, c Client, ref ProfileRef, note string) error {
	if n := len([]rune(note)); n > MaxInvitationNote {
		return errs.New(errs.Validation, "the note is %d characters long; LinkedIn allows %d", n, MaxInvitationNote)
	}
	urn, err := MemberURN(ctx, c, ref)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"invitee": map[string]any{"inviteeUnion": map[string]any{"memberProfile": urn}},
	}
	// An empty note still uses up one of the limited personalized invitations.
	if note != "" {
		payload["customMessage"] = note
	}
	_, err = c.Post(ctx, "/voyagerRelationshipsDashMemberRelationships", voyager.Params{
		{Key: "action", Value: "verifyQuotaAndCreateV2"},
		{Key: "decorationId", Value: "com.linkedin.voyager.dash.deco.relationships.InvitationCreationResultWithInvitee-2"},
	}, payload)
	return err
}

// RemoveConnection removes a member from your connections.
func RemoveConnection(ctx context.Context, c Client, ref ProfileRef) error {
	urn, err := MemberURN(ctx, c, ref)
	if err != nil {
		return err
	}
	_, err = c.Post(ctx, "/voyagerRelationshipsDashMemberRelationships", voyager.Params{
		{Key: "action", Value: "removeFromMyConnections"},
		{Key: "decorationId", Value: "com.linkedin.voyager.dash.deco.relationships.MemberRelationship-21"},
	}, map[string]any{"connectionUrn": "urn:li:fsd_connection:" + memberIDFromURN(urn)})
	return err
}

// Connections returns your connections, most recently added first.
func Connections(ctx context.Context, c Client, limit int) ([]Connection, error) {
	if err := checkLimit(limit, MaxListItems); err != nil {
		return nil, err
	}
	return paginate(limit, func(start int) ([]Connection, int, error) {
		raw, err := c.Get(ctx, "/relationships/dash/connections", voyager.Params{
			{Key: "decorationId", Value: ConnectionsDecoration},
			{Key: "count", Value: strconv.Itoa(ListPageSize)},
			{Key: "q", Value: "search"},
			{Key: "sortType", Value: "RECENTLY_ADDED"},
			{Key: "start", Value: strconv.Itoa(start)},
		})
		if err != nil {
			return nil, 0, err
		}
		found, next := ParseConnections(raw, start)
		return found, next, nil
	})
}

// ParseConnections reads a page of connections that started at start, and
// returns where the next page starts.
func ParseConnections(raw []byte, start int) ([]Connection, int) {
	doc := normalized.Parse(raw)
	items, n, coll := collection(doc)
	if coll == nil {
		items = doc.OfType(".Connection")
		n = len(items)
	}
	var out []Connection
	for _, item := range items {
		person := personIn(doc, item, "connectedMemberResolutionResult", "connectedMember", "miniProfile")
		if person == nil {
			continue
		}
		name, headline, url, urn := personSummary(person)
		if name == "" {
			continue
		}
		out = append(out, Connection{Name: name, Headline: headline, URL: url, URN: urn, Since: millisDate(firstOf(item, "createdAt", "createdTime"))})
	}
	return out, pageEnd(coll, start, n, ListPageSize)
}

func firstOf(entity normalized.Object, keys ...string) any {
	for _, key := range keys {
		if v, ok := entity[key]; ok && v != nil {
			return v
		}
	}
	return nil
}
