package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/api"
)

func TestWrapKeepsParagraphsAndWidth(t *testing.T) {
	lines := Wrap("one two three four five\n\nsix", 12, "  ")
	want := []string{"  one two", "  three four", "  five", "", "  six"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("Wrap = %q", lines)
	}
}

func TestFeedPostTruncatesUnlessFull(t *testing.T) {
	n := 4
	post := api.FeedPost{
		Author: "Grace Hopper", Age: "3h", AuthorHeadline: "Engineer",
		Text:      strings.Repeat("line\n", 20),
		Reactions: &n, URL: "https://www.linkedin.com/feed/update/urn:li:activity:1/",
	}
	short := FeedPost(post, false)
	if !strings.Contains(short, "Grace Hopper · 3h") || !strings.Contains(short, "--full") || !strings.Contains(short, "4 reactions") {
		t.Errorf("unexpected output:\n%s", short)
	}
	if strings.Contains(short, "comments") {
		t.Error("unknown counts should not be shown")
	}
	if full := FeedPost(post, true); strings.Contains(full, "--full") || strings.Count(full, "line") != 20 {
		t.Errorf("full output truncated:\n%s", full)
	}
}

func TestJSONDoesNotEscapeHTML(t *testing.T) {
	var buf bytes.Buffer
	_ = JSON(&buf, map[string]string{"text": "a < b & c"})
	if !strings.Contains(buf.String(), "a < b & c") {
		t.Errorf("JSON = %s", buf.String())
	}
}
