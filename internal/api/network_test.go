package api

import (
	"context"
	"strings"
	"testing"
)

func invitationsPage(t *testing.T) string {
	return doc(t, map[string]any{"*elements": []any{"urn:li:fs_relInvitationView:1", "urn:li:fs_relInvitationView:2"}},
		map[string]any{"entityUrn": "urn:li:fs_relInvitationView:1", "*invitation": "urn:li:fs_relInvitation:7001"},
		map[string]any{"entityUrn": "urn:li:fs_relInvitationView:2", "*invitation": "urn:li:fs_relInvitation:7002"},
		map[string]any{
			"$type": "com.linkedin.voyager.relationships.invitation.Invitation", "entityUrn": "urn:li:fs_relInvitation:7001",
			"sharedSecret": "s3cret", "message": "Hi Ada!", "sentTime": 1700000000000, "*fromMember": "urn:li:fs_miniProfile:ACoAAG",
		},
		map[string]any{
			"$type": "com.linkedin.voyager.relationships.invitation.Invitation", "entityUrn": "urn:li:fs_relInvitation:7002",
			"sharedSecret": "other", "*fromMember": "urn:li:fs_miniProfile:ACoAAH",
		},
		map[string]any{"entityUrn": "urn:li:fs_miniProfile:ACoAAG", "firstName": "Grace", "lastName": "Hopper", "occupation": "Admiral", "publicIdentifier": "grace"},
		map[string]any{"entityUrn": "urn:li:fs_miniProfile:ACoAAH", "firstName": "Alan", "lastName": "Turing"},
	)
}

func TestParseInvitations(t *testing.T) {
	invitations, n := ParseInvitations([]byte(invitationsPage(t)), "fromMember", 0)
	if n != 2 || len(invitations) != 2 {
		t.Fatalf("got %d of %d", len(invitations), n)
	}
	first := invitations[0]
	if first.ID != "7001" || first.Secret != "s3cret" || first.Name != "Grace Hopper" || first.Headline != "Admiral" ||
		first.Message != "Hi Ada!" || first.Sent != "2023-11-14" || first.Profile != "https://www.linkedin.com/in/grace/" {
		t.Errorf("invitation %+v", first)
	}
}

func TestAnsweringAnInvitationSendsItsSecret(t *testing.T) {
	r := &routes{replies: map[string]string{
		"/relationships/invitationViews":       invitationsPage(t),
		"POST /relationships/invitations/7002": `{}`,
	}}
	inv, err := FindInvitation(context.Background(), r, "7002")
	if err != nil || inv.Name != "Alan Turing" {
		t.Fatalf("invitation %+v, %v", inv, err)
	}
	if err := RespondToInvitation(context.Background(), r, inv, Accept); err != nil {
		t.Fatal(err)
	}
	last := r.last(t)
	if last.param("action") != "accept" || !strings.Contains(last.body(), `"invitationSharedSecret":"other"`) {
		t.Errorf("accept request %+v %s", last, last.body())
	}
	if _, err := FindInvitation(context.Background(), &routes{replies: map[string]string{
		"/relationships/invitationViews": doc(t, map[string]any{"*elements": []any{}}),
	}}, "1"); err == nil || !strings.Contains(err.Error(), "no received invitation") {
		t.Errorf("missing invitation: %v", err)
	}
}

func TestConnectLooksUpTheMemberAndChecksTheNote(t *testing.T) {
	r := &routes{replies: map[string]string{
		"/identity/dash/profiles": doc(t, map[string]any{"*elements": []any{"urn:li:fsd_profile:ACoAAB"}},
			map[string]any{"entityUrn": "urn:li:fsd_profile:ACoAAB", "firstName": "Ada", "publicIdentifier": "ada"}),
		"POST /voyagerRelationshipsDashMemberRelationships": `{}`,
	}}
	if err := Connect(context.Background(), r, ProfileRef{PublicID: "ada"}, "Hello"); err != nil {
		t.Fatal(err)
	}
	body := r.last(t).body()
	if !strings.Contains(body, `"memberProfile":"urn:li:fsd_profile:ACoAAB"`) || !strings.Contains(body, `"customMessage":"Hello"`) {
		t.Errorf("connect payload %s", body)
	}
	if err := Connect(context.Background(), r, ProfileRef{MemberID: "ACoAAB"}, strings.Repeat("x", MaxInvitationNote+1)); err == nil {
		t.Error("expected an error for a long note")
	}
}

func TestParseConnections(t *testing.T) {
	raw := doc(t, map[string]any{"*elements": []any{"urn:li:fsd_connection:1"}},
		map[string]any{"entityUrn": "urn:li:fsd_connection:1", "createdAt": 1700000000000, "*connectedMemberResolutionResult": "urn:li:fsd_profile:ACoAAG"},
		map[string]any{"entityUrn": "urn:li:fsd_profile:ACoAAG", "firstName": "Grace", "lastName": "Hopper", "headline": "Admiral", "publicIdentifier": "grace"},
	)
	connections, n := ParseConnections([]byte(raw), 0)
	if n != 1 || len(connections) != 1 {
		t.Fatalf("got %d of %d", len(connections), n)
	}
	if c := connections[0]; c.Name != "Grace Hopper" || c.Headline != "Admiral" || c.URN != "urn:li:fsd_profile:ACoAAG" || c.Since != "2023-11-14" {
		t.Errorf("connection %+v", c)
	}
}

func TestParseNotifications(t *testing.T) {
	raw := doc(t, map[string]any{"*elements": []any{"urn:li:fsd_notificationCard:1", "urn:li:fsd_notificationCard:2"}},
		map[string]any{
			"entityUrn": "urn:li:fsd_notificationCard:1", "headline": map[string]any{"text": "Grace commented on your post"},
			"publishedAt": 1700000000000, "read": false, "cardAction": map[string]any{"actionTarget": "https://www.linkedin.com/feed/update/urn:li:activity:1/"},
		},
		map[string]any{"entityUrn": "urn:li:fsd_notificationCard:2", "read": true},
	)
	notifications, n := ParseNotifications([]byte(raw), 0)
	if n != 2 || len(notifications) != 1 {
		t.Fatalf("got %d of %d", len(notifications), n)
	}
	if got := notifications[0]; got.Text != "Grace commented on your post" || got.Read || !strings.Contains(got.URL, "activity:1") {
		t.Errorf("notification %+v", got)
	}
}

func TestListsCheckTheirLimit(t *testing.T) {
	for _, list := range []func() error{
		func() error { _, err := Invitations(context.Background(), &routes{}, 0); return err },
		func() error { _, err := Connections(context.Background(), &routes{}, MaxListItems+1); return err },
		func() error { _, err := Notifications(context.Background(), &routes{}, -1); return err },
	} {
		if err := list(); err == nil {
			t.Error("expected a usage error")
		}
	}
}
