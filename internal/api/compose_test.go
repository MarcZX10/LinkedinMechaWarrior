package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tempFile(t *testing.T, name string, size int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPublishWithImagesUploadsThemFirst(t *testing.T) {
	r := &routes{replies: map[string]string{
		"POST /voyagerVideoDashMediaUploadMetadata": `{"data":{"value":{"type":"SINGLE","urn":"urn:li:digitalmediaAsset:A1",` +
			`"singleUploadUrl":"https://www.linkedin.com/dms-uploads/sp/A1","singleUploadHeaders":{"media-type-family":"STILLIMAGE"}}}}`,
		"POST /contentcreation/normShares": `{"data":{"*updateV2":"urn:li:fs_updateV2:(urn:li:activity:5,FEED_DETAIL)"}}`,
	}}
	image, err := NewAttachment(tempFile(t, "engine.png", 2048), Image)
	if err != nil {
		t.Fatal(err)
	}
	image.Alt = "The Analytical Engine"
	post, err := Publish(context.Background(), r, "Look", Anyone, PostOptions{Attachments: []Attachment{image}})
	if err != nil || post.URN != "urn:li:activity:5" {
		t.Fatalf("post %+v, %v", post, err)
	}
	if len(r.calls) != 3 {
		t.Fatalf("expected register, upload and share, got %+v", r.calls)
	}
	register, put, share := r.calls[0], r.calls[1], r.calls[2]
	if !strings.Contains(register.body(), `"mediaUploadType":"IMAGE_SHARING"`) || !strings.Contains(register.body(), `"fileSize":2048`) {
		t.Errorf("register %s", register.body())
	}
	if put.method != "PUT" || put.path != "https://www.linkedin.com/dms-uploads/sp/A1" ||
		!strings.Contains(put.body(), `"content-type":"image/png"`) || !strings.Contains(put.body(), `"media-type-family":"STILLIMAGE"`) {
		t.Errorf("upload %+v %s", put, put.body())
	}
	if !strings.Contains(share.body(), `"mediaUrn":"urn:li:digitalmediaAsset:A1"`) || !strings.Contains(share.body(), `"altText":"The Analytical Engine"`) ||
		!strings.Contains(share.body(), `"category":"IMAGE"`) {
		t.Errorf("share %s", share.body())
	}
}

func TestPublishWithAPollAndScheduled(t *testing.T) {
	r := &routes{replies: map[string]string{
		"POST /graphql":                    `{"data":{"data":{"createFeedDashPollsPollSummary":{"entityUrn":"urn:li:fsd_pollSummary:77"}}}}`,
		"POST /contentcreation/normShares": `{"data":{"status":{"urn":"urn:li:share:9"}}}`,
	}}
	at := time.Now().Add(48 * time.Hour).Truncate(time.Minute)
	poll := &Poll{Question: "Tabs or spaces?", Options: []string{"Tabs", "Spaces"}, Duration: "1w"}
	post, err := Publish(context.Background(), r, "Settle this", Anyone, PostOptions{Poll: poll, At: at})
	if err != nil || post.Scheduled == "" {
		t.Fatalf("post %+v, %v", post, err)
	}
	if body := r.calls[0].body(); !strings.Contains(body, `"duration":"ONE_WEEK"`) || !strings.Contains(r.calls[0].param("queryId"), "voyagerFeedDashPollsPollSummary.") {
		t.Errorf("poll %s", body)
	}
	share := r.calls[1].body()
	if !strings.Contains(share, `"mediaUrn":"urn:li:fsd_pollSummary:77"`) || !strings.Contains(share, `"category":"URN_REFERENCE"`) ||
		!strings.Contains(share, `"postState":"SCHEDULED"`) || !strings.Contains(share, `"scheduledAt":`) {
		t.Errorf("share %s", share)
	}

	refused := &routes{replies: map[string]string{"POST /graphql": `{"errors":[{"message":"Option too long"}]}`}}
	if _, err := Publish(context.Background(), refused, "x", Anyone, PostOptions{Poll: poll}); err == nil ||
		!strings.Contains(err.Error(), "Option too long") || !strings.Contains(err.Error(), "nothing was posted") {
		t.Errorf("a refused poll must stop the post: %v", err)
	}
	if len(refused.calls) != 1 {
		t.Errorf("the post was sent after the poll failed: %+v", refused.calls)
	}
}

func TestLinkPreviewAttachesTheArticleCard(t *testing.T) {
	r := &routes{replies: map[string]string{
		"/graphql": `{"data":{"data":{"contentcreationDashUpdateUrlPreviewByUrl":{"*elements":["urn:li:x:1"]}}},` +
			`"included":[{"entityUrn":"urn:li:x:1","content":{"articleComponent":{"articleUrn":"urn:li:article:42"}}}]}`,
		"POST /contentcreation/normShares": `{}`,
	}}
	if _, err := Publish(context.Background(), r, "Read this (https://example.com/post).", Anyone, PostOptions{LinkPreview: true}); err != nil {
		t.Fatal(err)
	}
	if vars := r.calls[0].param("variables"); vars != "(url:https%3A%2F%2Fexample.com%2Fpost)" {
		t.Errorf("preview variables %s", vars)
	}
	if share := r.calls[1].body(); !strings.Contains(share, `"category":"ARTICLE"`) || !strings.Contains(share, `"urn:li:article:42"`) {
		t.Errorf("share %s", share)
	}
}

func TestPostOptionsAreChecked(t *testing.T) {
	now := time.Now()
	image, _ := NewAttachment(tempFile(t, "a.jpg", 10), Image)
	doc, _ := NewAttachment(tempFile(t, "deck.pdf", 10), Document)
	for name, opts := range map[string]PostOptions{
		"two kinds":         {Attachments: []Attachment{image}, LinkPreview: true},
		"untitled document": {Attachments: []Attachment{doc}},
		"one option poll":   {Poll: &Poll{Question: "Q?", Options: []string{"A"}, Duration: "1d"}},
		"long poll option":  {Poll: &Poll{Question: "Q?", Options: []string{"A", strings.Repeat("b", 31)}, Duration: "1d"}},
		"bad duration":      {Poll: &Poll{Question: "Q?", Options: []string{"A", "B"}, Duration: "1y"}},
		"too soon":          {At: now.Add(time.Minute)},
		"too late":          {At: now.Add(200 * 24 * time.Hour)},
	} {
		if err := opts.Check(now); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	doc.Title = "Engine notes"
	if err := (PostOptions{Attachments: []Attachment{doc}, At: now.Add(time.Hour)}).Check(now); err != nil {
		t.Errorf("a titled, scheduled document is fine: %v", err)
	}
	if _, err := NewAttachment(tempFile(t, "notes.txt", 10), Image); err == nil {
		t.Error("a .txt file is not an image")
	}
	if _, err := ParseSchedule("2026-11-05 09:30", now); err != nil {
		t.Error(err)
	}
	if _, err := ParseSchedule("tomorrow", now); err == nil {
		t.Error("expected an error for an unreadable time")
	}
}

func TestThreadsEditsAndReplies(t *testing.T) {
	for input, want := range map[string]string{
		"urn:li:fsd_comment:(7001,urn:li:activity:42)": "urn:li:comment:(activity:42,7001)",
		"urn:li:comment:(activity:42,7001)":            "urn:li:comment:(activity:42,7001)",
		"urn:li:activity:42":                           "urn:li:activity:42",
	} {
		if got, err := ParseThread(input); err != nil || got != want {
			t.Errorf("ParseThread(%q) = %q, %v", input, got, err)
		}
	}
	if !IsComment("urn:li:comment:(activity:42,7001)") || IsComment("urn:li:activity:42") {
		t.Error("IsComment")
	}
	r := &routes{replies: map[string]string{
		"POST /contentcreation/normShares/urn:li:share:9": `{}`,
		"POST /voyagerSocialDashNormComments":             `{}`,
	}}
	if err := EditPost(context.Background(), r, "urn:li:share:9", "Fixed typo"); err != nil {
		t.Fatal(err)
	}
	if body := r.last(t).body(); body != `{"patch":{"$set":{"commentaryV2":{"attributes":[],"text":"Fixed typo"}}}}` {
		t.Errorf("edit %s", body)
	}
	if _, err := CommentOn(context.Background(), r, "urn:li:comment:(activity:42,7001)", "Agreed"); err != nil {
		t.Fatal(err)
	}
	if body := r.last(t).body(); !strings.Contains(body, `"threadUrn":"urn:li:comment:(activity:42,7001)"`) {
		t.Errorf("reply %s", body)
	}
	if FirstLink("See https://example.com/a?b=1.") != "https://example.com/a?b=1" {
		t.Error("FirstLink should drop the full stop")
	}
}
