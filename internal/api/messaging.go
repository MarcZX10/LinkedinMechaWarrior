package api

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/normalized"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/voyager"
)

// MaxMessage is a generous cap for one message; LinkedIn's own limit is
// higher, but a longer message is more likely a mistake.
const MaxMessage = 8000

// Mailbox folders, as the website names them.
var MessageFolders = map[string]string{
	"inbox":    "PRIMARY_INBOX",
	"other":    "SECONDARY_INBOX",
	"archive":  "ARCHIVE",
	"requests": "MESSAGE_REQUEST_PENDING",
	"spam":     "SPAM",
}

// Conversation is one thread of your inbox.
type Conversation struct {
	ID          string   `json:"id"`
	With        []string `json:"with"`
	Title       string   `json:"title,omitempty"`
	LastMessage string   `json:"last_message,omitempty"`
	LastSender  string   `json:"last_sender,omitempty"`
	Date        string   `json:"date,omitempty"`
	Unread      int      `json:"unread"`
	URL         string   `json:"url"`
}

// Message is one message of a conversation.
type Message struct {
	Sender string `json:"sender,omitempty"`
	Text   string `json:"text"`
	Date   string `json:"date,omitempty"`
}

// threadID is the id of a conversation in its address and URN, such as
// 2-ZTU4MDlhMDYtM2QyOC00YjM2LWE4NGYtZWIyOGQyNjE0ZTA0XzEwMA==.
var threadID = regexp.MustCompile(`2-[A-Za-z0-9_=%+/-]+`)

var memberURN = regexp.MustCompile(`urn:li:fsd_profile:[A-Za-z0-9_-]+`)

// ParseConversation returns the id of a conversation from its id, URN or
// address (linkedin.com/messaging/thread/ID/).
func ParseConversation(input string) (string, error) {
	input = strings.TrimSpace(input)
	if decoded, err := url.PathUnescape(input); err == nil {
		input = decoded
	}
	if m := threadID.FindString(input); m != "" {
		return strings.TrimRight(m, "/"), nil
	}
	return "", errs.New(errs.Usage, "%q is not a LinkedIn conversation; use its id from `lmw messages` or its address", input)
}

// MyURN returns your own member URN, which mailbox requests need.
func MyURN(ctx context.Context, c Client) (string, error) {
	me, err := Me(ctx, c)
	if err != nil {
		return "", err
	}
	if id := memberIDFromURN(me.URN); id != "" {
		return "urn:li:fsd_profile:" + id, nil
	}
	return "", errs.New(errs.API, "LinkedIn did not return your member id; inspect it with `lmw api get /me`")
}

func conversationURN(me, id string) string {
	return "urn:li:msg_conversation:(" + me + "," + id + ")"
}

// Conversations returns the latest conversations of a folder (see
// MessageFolders), most recent first.
func Conversations(ctx context.Context, c Client, folder string, limit int) ([]Conversation, error) {
	if err := checkLimit(limit, MaxListItems); err != nil {
		return nil, err
	}
	category, ok := MessageFolders[folder]
	if !ok {
		return nil, errs.New(errs.Usage, "unknown folder %q; use inbox, other, archive, requests or spam", folder)
	}
	me, err := MyURN(ctx, c)
	if err != nil {
		return nil, err
	}
	var conversations []Conversation
	cursor := ""
	for len(conversations) < limit {
		count := min(limit-len(conversations), 20)
		variables := fmt.Sprintf("(query:(predicateUnions:List((conversationCategoryPredicate:(category:%s)))),count:%d,mailboxUrn:%s",
			category, count, restliValue(me))
		if cursor != "" {
			variables += ",nextCursor:" + restliValue(cursor)
		}
		raw, err := conversationsQuery.Get(ctx, c, variables+")")
		if err != nil {
			return nil, err
		}
		found, next := ParseConversations(raw, me)
		conversations = append(conversations, found...)
		if len(found) == 0 || next == "" || next == cursor {
			break
		}
		cursor = next
	}
	if len(conversations) > limit {
		conversations = conversations[:limit]
	}
	return conversations, nil
}

// ParseConversations reads a page of conversations and the cursor of the
// next page. me is left out of each conversation's participants.
func ParseConversations(raw []byte, me string) ([]Conversation, string) {
	doc := normalized.Parse(raw)
	items, _, coll := collection(doc)
	if coll == nil {
		items = doc.OfType(".Conversation")
	}
	var out []Conversation
	for _, item := range items {
		id := threadID.FindString(normalized.String(item["entityUrn"]))
		if id == "" {
			continue
		}
		conv := Conversation{ID: id, Title: normalized.Text(item["title"]), URL: "https://www.linkedin.com/messaging/thread/" + id + "/"}
		for _, p := range doc.Refs(item, "conversationParticipants") {
			if participantURN(p) != me {
				if name := participantName(doc, p); name != "" {
					conv.With = append(conv.With, name)
				}
			}
		}
		if conv.With == nil {
			conv.With = []string{}
		}
		if unread, ok := normalized.Int(item["unreadCount"]); ok {
			conv.Unread = unread
		}
		conv.Date = millisDate(item["lastActivityAt"])
		if messages := doc.Refs(item, "messages"); len(messages) > 0 {
			last := messages[0]
			conv.LastMessage = normalized.Text(last["body"])
			conv.LastSender = participantName(doc, personIn(doc, last, "sender"))
		}
		out = append(out, conv)
	}
	next, _ := normalized.Dig(coll, "metadata", "nextCursor").(string)
	return out, next
}

// participantURN is the member URN of a messaging participant.
func participantURN(p normalized.Object) string {
	for _, candidate := range []string{normalized.String(p["hostIdentityUrn"]), normalized.String(p["entityUrn"])} {
		if m := memberURN.FindString(candidate); m != "" {
			return m
		}
	}
	return ""
}

// participantName reads the name of a messaging participant: a member or an
// organization.
func participantName(doc *normalized.Doc, p normalized.Object) string {
	if p == nil {
		return ""
	}
	member, _ := normalized.Dig(p, "participantType", "member").(normalized.Object)
	if member != nil {
		return strings.TrimSpace(normalized.Text(member["firstName"]) + " " + normalized.Text(member["lastName"]))
	}
	if org, _ := normalized.Dig(p, "participantType", "organization").(normalized.Object); org != nil {
		return normalized.Text(org["name"])
	}
	return normalized.Text(p["name"])
}

// Messages returns the latest messages of a conversation, oldest first.
func Messages(ctx context.Context, c Client, conversation string, limit int) ([]Message, error) {
	if err := checkLimit(limit, MaxListItems); err != nil {
		return nil, err
	}
	me, err := MyURN(ctx, c)
	if err != nil {
		return nil, err
	}
	raw, err := messagesQuery.Get(ctx, c, "(conversationUrn:"+restliValue(conversationURN(me, conversation))+")")
	if err != nil {
		return nil, err
	}
	messages := ParseMessages(raw)
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
}

// ParseMessages reads the messages of a conversation, oldest first.
func ParseMessages(raw []byte) []Message {
	doc := normalized.Parse(raw)
	items, _, coll := collection(doc)
	if coll == nil {
		items = doc.OfType(".Message")
	}
	type dated struct {
		Message
		at int
	}
	var all []dated
	for _, item := range items {
		text := normalized.Text(item["body"])
		if text == "" {
			continue
		}
		at, _ := normalized.Int(item["deliveredAt"])
		all = append(all, dated{Message{
			Sender: participantName(doc, personIn(doc, item, "sender", "actor")),
			Text:   text,
			Date:   millisDateTime(item["deliveredAt"]),
		}, at})
	}
	// Oldest first, like a chat.
	sort.SliceStable(all, func(i, j int) bool { return all[i].at < all[j].at })
	out := make([]Message, len(all))
	for i, m := range all {
		out[i] = m.Message
	}
	return out
}

// SendMessage sends a message to an existing conversation (conversation set)
// or starts one with a member (to set). It returns the conversation id when
// LinkedIn tells it.
func SendMessage(ctx context.Context, c Client, conversation string, to ProfileRef, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errs.New(errs.Validation, "the message is empty")
	}
	if n := len([]rune(text)); n > MaxMessage {
		return "", errs.New(errs.Validation, "the message is %d characters long; lmw sends at most %d", n, MaxMessage)
	}
	me, err := MyURN(ctx, c)
	if err != nil {
		return "", err
	}
	message := map[string]any{
		"body":                map[string]any{"attributes": []any{}, "text": text},
		"renderContentUnions": []any{},
		"originToken":         uuid4(),
	}
	payload := map[string]any{
		"message":                      message,
		"mailboxUrn":                   me,
		"trackingId":                   randomToken(16),
		"dedupeByClientGeneratedToken": false,
	}
	if conversation != "" {
		message["conversationUrn"] = conversationURN(me, conversation)
	} else {
		recipient, err := MemberURN(ctx, c, to)
		if err != nil {
			return "", err
		}
		payload["hostRecipientUrns"] = []string{recipient}
	}
	raw, err := c.Post(ctx, "/voyagerMessagingDashMessengerMessages", voyager.Params{{Key: "action", Value: "createMessage"}}, payload)
	if err != nil {
		return "", err
	}
	if id := threadID.FindString(string(raw)); id != "" {
		return id, nil
	}
	return conversation, nil
}

// MarkRead marks a conversation as read.
func MarkRead(ctx context.Context, c Client, conversation string) error {
	me, err := MyURN(ctx, c)
	if err != nil {
		return err
	}
	path := "/voyagerMessagingDashMessengerConversations/" + url.PathEscape(conversationURN(me, conversation))
	_, err = c.Post(ctx, path, nil, map[string]any{"patch": map[string]any{"$set": map[string]any{"read": true}}})
	return err
}

func millisDateTime(value any) string {
	ms, ok := normalized.Int(value)
	if !ok || ms <= 0 {
		return ""
	}
	return time.UnixMilli(int64(ms)).Local().Format("2006-01-02 15:04")
}

func uuid4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// randomToken returns n random letters and digits.
func randomToken(n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}
