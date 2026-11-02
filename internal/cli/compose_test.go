package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPublishWithAnImageUploadsAfterConfirming(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	image := filepath.Join(t.TempDir(), "engine.png")
	_ = os.WriteFile(image, make([]byte, 1500), 0o600)
	h.linkedin.replies["/voyagerVideoDashMediaUploadMetadata"] = reply{status: 200, body: `{"data":{"value":{"type":"SINGLE",` +
		`"urn":"urn:li:digitalmediaAsset:A1","singleUploadUrl":"https://www.linkedin.com/dms-uploads/sp/A1","singleUploadHeaders":{"media-type-family":"STILLIMAGE"}}}}`}
	h.linkedin.replies["/dms-uploads/sp/A1"] = reply{status: 201}

	before := len(h.linkedin.requests)
	out := h.mustRun("no\n", "post", "publish", "--text", "Our engine", "--image", image, "--alt", "The engine")
	if !strings.Contains(out, "Attached: image engine.png (1 KB), alt text: The engine") || len(h.linkedin.requests) != before {
		t.Errorf("a cancelled post must upload nothing: %s", out)
	}

	out = h.mustRun("publish\n", "post", "publish", "--text", "Our engine", "--image", image, "--alt", "The engine")
	if !strings.Contains(out, "Published: https://www.linkedin.com/feed/update/urn:li:activity:99/") {
		t.Errorf("publish: %s", out)
	}
	sent := h.linkedin.requests[before:]
	if len(sent) != 3 || sent[1].Method != "PUT" || sent[1].Header["media-type-family"][0] != "STILLIMAGE" || sent[1].Header["content-type"][0] != "image/png" {
		t.Fatalf("expected register, upload and share; got %d requests", len(sent))
	}
	if share := h.linkedin.bodies[len(h.linkedin.bodies)-1]; !strings.Contains(share, `"mediaUrn":"urn:li:digitalmediaAsset:A1"`) {
		t.Errorf("share payload %s", share)
	}

	for _, args := range [][]string{
		{"--image", image, "--poll", "Q?", "--option", "A", "--option", "B"},
		{"--document", image},
		{"--option", "A"},
		{"--at", "yesterday"},
	} {
		if code, _, _ := h.run("", append([]string{"post", "publish", "--text", "x"}, args...)...); code != 2 && code != 1 {
			t.Errorf("publish %v should fail, exit %d", args, code)
		}
	}
}

func TestScheduledPollPost(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.linkedin.replies["/graphql"] = reply{status: 200, body: `{"data":{"data":{"x":{"entityUrn":"urn:li:fsd_pollSummary:5"}}}}`}
	at := time.Now().Add(26 * time.Hour).Format("2006-01-02 15:04")
	out := h.mustRun("publish\n", "post", "publish", "--text", "Settle this", "--poll", "Tabs or spaces?",
		"--option", "Tabs", "--option", "Spaces", "--duration", "3d", "--at", at, "--json")
	var post map[string]any
	if err := json.Unmarshal([]byte(out), &post); err != nil || post["scheduled_for"] == nil {
		t.Fatalf("scheduled post: %s %v", out, err)
	}
	share := h.linkedin.bodies[len(h.linkedin.bodies)-1]
	if !strings.Contains(share, `"postState":"SCHEDULED"`) || !strings.Contains(share, `"urn:li:fsd_pollSummary:5"`) {
		t.Errorf("share payload %s", share)
	}
}

func TestEditReplyAndReactToAComment(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.linkedin.replies["/contentcreation/normShares/urn:li:activity:42"] = reply{status: 200, body: `{}`}
	if out := h.mustRun("edit\n", "post", "edit", "urn:li:activity:42", "--text", "Fixed"); !strings.Contains(out, "Post edited") {
		t.Errorf("edit: %s", out)
	}
	if last := h.linkedin.bodies[len(h.linkedin.bodies)-1]; !strings.Contains(last, `"$set":{"commentaryV2":{"attributes":[],"text":"Fixed"}}`) {
		t.Errorf("edit payload %s", last)
	}

	comment := "urn:li:fsd_comment:(7001,urn:li:activity:42)"
	h.linkedin.replies["/voyagerSocialDashNormComments"] = reply{status: 201, body: `{}`}
	if out := h.mustRun("reply\n", "post", "reply", comment, "--text", "Thanks!"); !strings.Contains(out, "Reply posted") {
		t.Errorf("reply: %s", out)
	}
	if last := h.linkedin.bodies[len(h.linkedin.bodies)-1]; !strings.Contains(last, `"threadUrn":"urn:li:comment:(activity:42,7001)"`) {
		t.Errorf("reply payload %s", last)
	}
	if code, _, _ := h.run("", "post", "reply", "urn:li:activity:42", "--text", "x"); code != 2 {
		t.Error("replying to a post should be a usage error")
	}

	h.linkedin.replies["/graphql"] = reply{status: 200, body: `{}`}
	h.mustRun("react\n", "post", "react", comment, "--type", "insightful")
	if last := h.linkedin.bodies[len(h.linkedin.bodies)-1]; !strings.Contains(last, `"threadUrn":"urn:li:comment:(activity:42,7001)"`) || !strings.Contains(last, `"INTEREST"`) {
		t.Errorf("comment reaction payload %s", last)
	}
}

func TestDoctor(t *testing.T) {
	h := newHarness(t)
	if code, out, _ := h.run("", "doctor"); code == 0 || !strings.Contains(out, "✘ session") || !strings.Contains(out, "lmw auth import") {
		t.Errorf("doctor without a session: exit %d\n%s", code, out)
	}
	h.importPasted()
	out := h.mustRun("", "doctor", "--online")
	for _, want := range []string{"✔ session", "✔ identity", "✔ budget", "✔ cooldown", "✔ queries", "✔ linkedin", "logged in as Ada Lovelace", "All systems go."} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor is missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorProbeReadsEveryFeatureAndStopsOnPushback(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	empty := reply{status: 200, body: `{"data":{"*elements":[]},"included":[]}`}
	for _, path := range []string{"/voyagerIdentityDashNotificationCards", "/relationships/invitationViews", "/relationships/dash/connections",
		"/voyagerMessagingGraphQL/graphql", "/voyagerJobsDashJobCards"} {
		h.linkedin.replies[path] = empty
	}
	// Search (and saved posts, the same query) find the person themself.
	h.linkedin.replies["/graphql"] = reply{status: 200, body: `{"data":{"data":{"searchDashClustersByAll":{"elements":[{"items":[{"item":{"*entityResult":"urn:li:r:1"}}]}]}}},"included":[` +
		`{"entityUrn":"urn:li:r:1","title":{"text":"Ada Lovelace"}}]}`}
	h.linkedin.replies["/identity/dash/profiles"] = reply{status: 200, body: dashProfileJSON}
	h.linkedin.replies["/organization/companies"] = reply{status: 200, body: `{"data":{"*elements":["urn:li:fs_normalized_company:1"]},"included":[` +
		`{"$type":"com.linkedin.voyager.organization.Company","entityUrn":"urn:li:fs_normalized_company:1","name":"LinkedIn","universalName":"linkedin"}]}`}

	out := h.mustRun("", "doctor", "--probe")
	for _, name := range []string{"me", "feed", "profile", "notifications", "invitations", "connections", "messages", "search", "saved", "jobs", "company"} {
		if !strings.Contains(out, "✔ probe "+name) {
			t.Errorf("probe %s did not pass:\n%s", name, out)
		}
	}

	h.linkedin.replies["/graphql"] = empty
	if code, out, _ := h.run("", "doctor", "--probe"); code == 0 || !strings.Contains(out, "✘ probe search") || !strings.Contains(out, "found nobody") {
		t.Errorf("an empty search must fail the probe: exit %d\n%s", code, out)
	}

	h.linkedin.replies["/feed/updatesV2"] = reply{status: 999}
	before := len(h.linkedin.requests)
	code, out, _ := h.run("", "doctor", "--probe")
	if code == 0 || !strings.Contains(out, "✘ probe feed") || !strings.Contains(out, "stopped probing") {
		t.Errorf("a rate limit must stop the probes: exit %d\n%s", code, out)
	}
	if sent := len(h.linkedin.requests) - before; sent != 2 {
		t.Errorf("after the rate limit no request may follow; %d were sent", sent)
	}
}
