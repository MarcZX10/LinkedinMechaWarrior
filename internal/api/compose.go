package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/normalized"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

// What a post can carry besides text, as public clients verified against the
// web app in 2026: images, a video or a PDF uploaded first (register, then
// PUT the bytes), a poll created first, or the card of a link. Uploading or
// creating these publishes nothing until a post references them.

const (
	// MaxImages is how many images one post may carry.
	MaxImages = 20
	// MaxUploadBytes caps the size of one file lmw uploads.
	MaxUploadBytes = 100 << 20
	// MaxPollOptions and the length limits are LinkedIn's poll rules.
	MaxPollOptions   = 4
	MaxPollQuestion  = 140
	MaxPollOption    = 30
	minScheduleAhead = 10 * time.Minute
	maxScheduleAhead = 90 * 24 * time.Hour
)

var (
	pollQuery = Query{
		Name: "voyagerFeedDashPollsPollSummary", Vars: []string{"poll"},
		Hashes: []string{"f8ad99cf791d833d37dddb373d06fb3a"},
	}
	urlPreviewQuery = Query{
		Name: "voyagerContentcreationDashUpdateUrlPreview", Vars: []string{"url"},
		Hashes: []string{"b092c1aea4b6c087ec0d09614b3b3320"},
	}
)

// MediaKind is what kind of file is attached.
type MediaKind string

// The kinds of files a post can carry.
const (
	Image    MediaKind = "IMAGE"
	Video    MediaKind = "VIDEO"
	Document MediaKind = "DOCUMENT"
)

var mediaTypes = map[string]struct {
	kind        MediaKind
	contentType string
}{
	".png": {Image, "image/png"}, ".jpg": {Image, "image/jpeg"}, ".jpeg": {Image, "image/jpeg"},
	".gif": {Image, "image/gif"}, ".webp": {Image, "image/webp"},
	".mp4": {Video, "video/mp4"}, ".mov": {Video, "video/quicktime"},
	".pdf": {Document, "application/pdf"},
}

// Attachment is a file to attach to a post.
type Attachment struct {
	Path string
	Kind MediaKind
	// Alt is the description of an image for screen readers.
	Alt string
	// Title names a document; LinkedIn requires one.
	Title       string
	contentType string
	size        int64
}

// NewAttachment checks a file and finds its kind from the extension.
func NewAttachment(path string, kind MediaKind) (Attachment, error) {
	ext := strings.ToLower(filepath.Ext(path))
	t, ok := mediaTypes[ext]
	if !ok || t.kind != kind {
		var accepted []string
		for e, m := range mediaTypes {
			if m.kind == kind {
				accepted = append(accepted, e)
			}
		}
		sort.Strings(accepted)
		return Attachment{}, errs.New(errs.Usage, "%s is not a supported %s; use %s", path, strings.ToLower(string(kind)), strings.Join(accepted, ", "))
	}
	info, err := os.Stat(path)
	if err != nil {
		return Attachment{}, errs.Wrap(errs.Usage, err, "cannot read %s: %v", path, err)
	}
	if info.Size() == 0 || info.Size() > MaxUploadBytes {
		return Attachment{}, errs.New(errs.Validation, "%s is %d bytes; lmw uploads files of up to %d MB", path, info.Size(), MaxUploadBytes>>20)
	}
	return Attachment{Path: path, Kind: kind, contentType: t.contentType, size: info.Size()}, nil
}

// Describe summarizes the attachment for a confirmation.
func (a Attachment) Describe() string {
	line := fmt.Sprintf("%s %s (%s)", strings.ToLower(string(a.Kind)), filepath.Base(a.Path), humanSize(a.size))
	if a.Alt != "" {
		line += ", alt text: " + a.Alt
	}
	if a.Title != "" {
		line += ", title: " + a.Title
	}
	return line
}

// Poll is a poll attached to a post.
type Poll struct {
	Question string
	Options  []string
	// Duration is "1d", "3d", "1w" or "2w".
	Duration string
}

var pollDurations = map[string]string{"1d": "ONE_DAY", "3d": "THREE_DAYS", "1w": "ONE_WEEK", "2w": "TWO_WEEKS"}

// PollDurations lists the poll durations, shortest first.
var PollDurations = []string{"1d", "3d", "1w", "2w"}

// Check validates a poll against LinkedIn's rules.
func (p Poll) Check() error {
	if q := len([]rune(strings.TrimSpace(p.Question))); q == 0 || q > MaxPollQuestion {
		return errs.New(errs.Validation, "a poll question needs 1 to %d characters", MaxPollQuestion)
	}
	if n := len(p.Options); n < 2 || n > MaxPollOptions {
		return errs.New(errs.Validation, "a poll needs 2 to %d options, got %d", MaxPollOptions, n)
	}
	for _, o := range p.Options {
		if n := len([]rune(strings.TrimSpace(o))); n == 0 || n > MaxPollOption {
			return errs.New(errs.Validation, "poll option %q must have 1 to %d characters", o, MaxPollOption)
		}
	}
	if _, ok := pollDurations[p.Duration]; !ok {
		return errs.New(errs.Usage, "unknown poll duration %q; use %s", p.Duration, strings.Join(PollDurations, ", "))
	}
	return nil
}

// PostOptions are everything a post can carry besides its text and audience.
type PostOptions struct {
	Attachments []Attachment
	Poll        *Poll
	// LinkPreview attaches the card of the first link in the text.
	LinkPreview bool
	// At schedules the post; LinkedIn publishes it then.
	At time.Time
}

// Check validates the combination of options.
func (o PostOptions) Check(now time.Time) error {
	kinds := map[MediaKind]int{}
	for _, a := range o.Attachments {
		kinds[a.Kind]++
		if a.Kind == Document && strings.TrimSpace(a.Title) == "" {
			return errs.New(errs.Usage, "a document needs a title (--title)")
		}
	}
	extras := len(kinds)
	if o.Poll != nil {
		extras++
	}
	if o.LinkPreview {
		extras++
	}
	switch {
	case extras > 1:
		return errs.New(errs.Usage, "a post carries one kind of extra: images, a video, a document, a poll or a link card")
	case kinds[Image] > MaxImages:
		return errs.New(errs.Usage, "a post can carry up to %d images", MaxImages)
	case kinds[Video] > 1 || kinds[Document] > 1:
		return errs.New(errs.Usage, "a post can carry one video or one document")
	}
	if o.Poll != nil {
		if err := o.Poll.Check(); err != nil {
			return err
		}
	}
	if !o.At.IsZero() {
		if o.At.Before(now.Add(minScheduleAhead)) || o.At.After(now.Add(maxScheduleAhead)) {
			return errs.New(errs.Usage, "a post can be scheduled from 10 minutes to 3 months ahead")
		}
	}
	return nil
}

// ParseSchedule reads a local date and time such as "2026-11-05 09:30".
func ParseSchedule(value string, now time.Time) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(value), now.Location()); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errs.New(errs.Usage, "cannot read the time %q; write it like 2026-11-05 09:30", value)
}

// shareMedia is one media entry of a post.
type shareMedia struct {
	category, urn, alt, title string
}

// SharePayload builds the body LinkedIn's web app sends to create a post.
// at is the scheduled time, or zero to publish now.
func SharePayload(text string, visibility Visibility, media []shareMedia, at time.Time) map[string]any {
	entries := []any{}
	for _, m := range media {
		entry := map[string]any{"category": m.category, "mediaUrn": m.urn, "tapTargets": []any{}}
		if m.alt != "" {
			entry["altText"] = m.alt
		}
		if m.title != "" {
			entry["title"] = map[string]any{"text": m.title}
		}
		entries = append(entries, entry)
	}
	payload := map[string]any{
		"visibleToConnectionsOnly":  visibility == ConnectionsOnly,
		"externalAudienceProviders": []any{},
		"commentaryV2": map[string]any{
			"text":       text,
			"attributes": []any{},
		},
		"origin":                 "FEED",
		"allowedCommentersScope": "ALL",
		"postState":              "PUBLISHED",
		"media":                  entries,
	}
	if !at.IsZero() {
		payload["postState"] = "SCHEDULED"
		payload["scheduledAt"] = at.UnixMilli()
	}
	return payload
}

// prepareMedia uploads attachments, creates the poll or resolves the link
// card, and returns the media entries of the post. Nothing is published.
func prepareMedia(ctx context.Context, c Client, text string, opts PostOptions) ([]shareMedia, error) {
	var media []shareMedia
	for _, a := range opts.Attachments {
		urn, err := upload(ctx, c, a)
		if err != nil {
			return nil, fmt.Errorf("%w; nothing was posted", err)
		}
		category := string(a.Kind)
		if a.Kind == Document {
			category = "NATIVE_DOCUMENT"
		}
		media = append(media, shareMedia{category: category, urn: urn, alt: a.Alt, title: a.Title})
	}
	if opts.Poll != nil {
		urn, err := createPoll(ctx, c, *opts.Poll)
		if err != nil {
			return nil, fmt.Errorf("%w; nothing was posted", err)
		}
		media = append(media, shareMedia{category: "URN_REFERENCE", urn: urn})
	}
	if opts.LinkPreview {
		link := FirstLink(text)
		if link == "" {
			return nil, errs.New(errs.Usage, "--link-preview needs a link in the text")
		}
		card, err := LinkCard(ctx, c, link)
		if err != nil {
			return nil, fmt.Errorf("%w; nothing was posted", err)
		}
		if card != "" {
			media = append(media, shareMedia{category: "ARTICLE", urn: card})
		}
	}
	return media, nil
}

// upload registers a file with LinkedIn, PUTs its bytes, and returns the
// asset URN.
func upload(ctx context.Context, c Client, a Attachment) (string, error) {
	file, err := os.ReadFile(a.Path)
	if err != nil {
		return "", errs.Wrap(errs.Usage, err, "cannot read %s: %v", a.Path, err)
	}
	raw, err := c.Post(ctx, "/voyagerVideoDashMediaUploadMetadata", voyager.Params{{Key: "action", Value: "upload"}}, map[string]any{
		"mediaUploadType": string(a.Kind) + "_SHARING",
		"fileSize":        len(file),
		"filename":        filepath.Base(a.Path),
	})
	if err != nil {
		return "", err
	}
	var ticket struct {
		Data struct {
			Value struct {
				Type            string            `json:"type"`
				URN             string            `json:"urn"`
				SingleUploadURL string            `json:"singleUploadUrl"`
				Headers         map[string]string `json:"singleUploadHeaders"`
			} `json:"value"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &ticket)
	v := ticket.Data.Value
	if v.Type != "" && v.Type != "SINGLE" {
		return "", errs.New(errs.API, "LinkedIn asked for a %s upload of %s, which lmw does not do; try a smaller file", v.Type, filepath.Base(a.Path))
	}
	if v.URN == "" || v.SingleUploadURL == "" {
		return "", errs.New(errs.API, "LinkedIn's upload ticket for %s had no address", filepath.Base(a.Path))
	}
	if err := c.Upload(ctx, v.SingleUploadURL, a.contentType, v.Headers, file); err != nil {
		return "", err
	}
	return v.URN, nil
}

var pollSummaryURN = regexp.MustCompile(`urn:li:fsd_pollSummary:\d+`)

func createPoll(ctx context.Context, c Client, p Poll) (string, error) {
	options := make([]string, len(p.Options))
	for i, o := range p.Options {
		options[i] = strings.TrimSpace(o)
	}
	raw, err := pollQuery.Execute(ctx, c, map[string]any{"poll": map[string]any{
		"question": strings.TrimSpace(p.Question), "options": options, "duration": pollDurations[p.Duration],
	}})
	if err != nil {
		return "", err
	}
	if msg := graphqlError(raw); msg != "" {
		return "", errs.New(errs.Validation, "LinkedIn refused the poll: %s", msg)
	}
	urn := pollSummaryURN.FindString(string(raw))
	if urn == "" {
		return "", errs.New(errs.API, "LinkedIn created no poll; its answer had no poll id")
	}
	return urn, nil
}

// graphqlError returns the first GraphQL error of an answer, which LinkedIn
// sends with HTTP 200.
func graphqlError(raw []byte) string {
	var answer struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Data struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &answer) != nil {
		return ""
	}
	for _, list := range [][]struct {
		Message string `json:"message"`
	}{answer.Errors, answer.Data.Errors} {
		if len(list) > 0 {
			return firstNonEmpty(list[0].Message, "GraphQL error")
		}
	}
	return ""
}

var linkInText = regexp.MustCompile(`https?://[^\s<>"]+`)

// FirstLink returns the first link of a text, without the punctuation that
// ends the sentence around it.
func FirstLink(text string) string {
	link := strings.TrimRight(linkInText.FindString(text), ".,;:!?'\"")
	if strings.HasSuffix(link, ")") && !strings.Contains(link, "(") {
		link = strings.TrimSuffix(link, ")")
	}
	return link
}

// LinkCard asks LinkedIn for the article card of a link and returns its URN,
// or "" when LinkedIn has no card for it.
func LinkCard(ctx context.Context, c Client, link string) (string, error) {
	raw, err := urlPreviewQuery.Get(ctx, c, "(url:"+restliValue(link)+")")
	if err != nil {
		return "", err
	}
	if msg := graphqlError(raw); msg != "" {
		return "", errs.New(errs.API, "LinkedIn could not preview %s: %s", link, msg)
	}
	doc := normalized.Parse(raw)
	if items, n, _ := collection(doc); n == 0 && len(items) == 0 {
		return "", nil
	}
	for _, entity := range doc.Included {
		if urn := normalized.String(normalized.Dig(entity, "content", "articleComponent", "articleUrn")); urn != "" {
			return urn, nil
		}
	}
	return "", nil
}

// EditPost replaces the text of one of your posts.
func EditPost(ctx context.Context, c Client, post, text string) error {
	_, err := c.Post(ctx, "/contentcreation/normShares/"+url.PathEscape(post), nil,
		map[string]any{"patch": map[string]any{"$set": map[string]any{"commentaryV2": map[string]any{"text": text, "attributes": []any{}}}}})
	return err
}

var (
	dashCommentURN  = regexp.MustCompile(`^urn:li:fsd_comment:\((\d+),urn:li:(activity|ugcPost|share):(\d+)\)$`)
	shortCommentURN = regexp.MustCompile(`^urn:li:comment:\((activity|ugcPost|share):(\d+),(\d+)\)$`)
)

// ParseThread returns what to react to or reply in: a post URN (see
// ParsePost), or a comment as urn:li:comment:(activity:POST,COMMENT), which
// is how the web app names a comment's thread. Comments can be given by the
// URN `lmw post comments --json` prints.
func ParseThread(input string) (string, error) {
	input = strings.TrimSpace(input)
	if m := dashCommentURN.FindStringSubmatch(input); m != nil {
		return "urn:li:comment:(" + m[2] + ":" + m[3] + "," + m[1] + ")", nil
	}
	if shortCommentURN.MatchString(input) {
		return input, nil
	}
	return ParsePost(input)
}

// IsComment reports whether a thread from ParseThread is a comment.
func IsComment(thread string) bool { return strings.HasPrefix(thread, "urn:li:comment:") }

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}
