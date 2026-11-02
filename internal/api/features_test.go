package api

import (
	"context"
	"strings"
	"testing"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

// graphqlDoc builds a GraphQL normalized response whose collection under key
// lists the URNs of the included entities.
func graphqlDoc(t *testing.T, key string, extra map[string]any, included ...map[string]any) string {
	t.Helper()
	var urns []any
	for _, e := range included {
		if urn, ok := e["entityUrn"].(string); ok && e["$listed"] != false {
			urns = append(urns, urn)
		}
		delete(e, "$listed")
	}
	coll := map[string]any{"*elements": urns}
	for k, v := range extra {
		coll[k] = v
	}
	return doc(t, map[string]any{"data": map[string]any{key: coll}}, included...)
}

func TestCommentsAndReactions(t *testing.T) {
	post := "urn:li:activity:42"
	r := &routes{replies: map[string]string{
		"/graphql": graphqlDoc(t, "socialDashCommentsBySocialDetail", nil,
			map[string]any{
				"$type": "com.linkedin.voyager.dash.social.Comment", "entityUrn": "urn:li:fsd_comment:(1,urn:li:activity:42)",
				"commenter":  map[string]any{"title": map[string]any{"text": "Grace Hopper"}, "subtitle": map[string]any{"text": "Admiral"}, "navigationUrl": "https://www.linkedin.com/in/grace?mini=1"},
				"commentary": map[string]any{"text": "Great engines"}, "createdAt": 1700000000000,
			}),
	}}
	comments, err := Comments(context.Background(), r, post, 5)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments %+v, %v", comments, err)
	}
	if c := comments[0]; c.Author != "Grace Hopper" || c.Text != "Great engines" || c.AuthorURL != "https://www.linkedin.com/in/grace" || c.Date != "2023-11-14" {
		t.Errorf("comment %+v", c)
	}
	vars := r.last(t).param("variables")
	if !strings.Contains(vars, "socialDetailUrn:urn%3Ali%3Afsd_socialDetail%3A%28urn%3Ali%3Aactivity%3A42%2Curn%3Ali%3Aactivity%3A42%2Curn%3Ali%3AhighlightedReply%3A-%29") {
		t.Errorf("comments variables %s", vars)
	}

	r.replies["/graphql"] = graphqlDoc(t, "socialDashReactionsByReactionType", nil,
		map[string]any{"entityUrn": "urn:li:fsd_reaction:1", "reactionType": "PRAISE",
			"reactorLockup": map[string]any{"title": map[string]any{"text": "Alan Turing"}, "subtitle": map[string]any{"text": "Mathematician"}}})
	reactions, err := Reactions(context.Background(), r, post, 5)
	if err != nil || len(reactions) != 1 || reactions[0].Type != "celebrate" || reactions[0].Name != "Alan Turing" {
		t.Errorf("reactions %+v, %v", reactions, err)
	}
}

func TestReactCommentAndDelete(t *testing.T) {
	r := &routes{replies: map[string]string{
		"POST /graphql":                                     `{}`,
		"POST /voyagerSocialDashNormComments":               `{"data":{"entityUrn":"urn:li:fsd_comment:(99,urn:li:activity:42)"}}`,
		"DELETE /contentcreation/normShares/urn:li:share:7": `{}`,
	}}
	if err := React(context.Background(), r, "urn:li:activity:42", "celebrate"); err != nil {
		t.Fatal(err)
	}
	last := r.last(t)
	if !strings.HasPrefix(last.param("queryId"), "voyagerSocialDashReactions.") || last.param("action") != "execute" ||
		!strings.Contains(last.body(), `"entity":{"reactionType":"PRAISE"}`) || !strings.Contains(last.body(), `"threadUrn":"urn:li:activity:42"`) {
		t.Errorf("react request %+v %s", last, last.body())
	}
	if err := React(context.Background(), r, "urn:li:activity:42", "meh"); err == nil {
		t.Error("expected an error for an unknown reaction")
	}
	urn, err := CommentOn(context.Background(), r, "urn:li:activity:42", "  Nice!  ")
	if err != nil || urn != "urn:li:fsd_comment:(99,urn:li:activity:42)" || !strings.Contains(r.last(t).body(), `"text":"Nice!"`) {
		t.Errorf("comment %q, %v, %s", urn, err, r.last(t).body())
	}
	if _, err := CommentOn(context.Background(), r, "urn:li:activity:42", strings.Repeat("x", MaxComment+1)); err == nil {
		t.Error("expected an error for a long comment")
	}
	if err := DeletePost(context.Background(), r, "urn:li:share:7"); err != nil {
		t.Errorf("delete: %v", err)
	}
}

func meDoc(t *testing.T) string {
	return doc(t, map[string]any{"*miniProfile": "urn:li:fs_miniProfile:ACoAAME"},
		map[string]any{"entityUrn": "urn:li:fs_miniProfile:ACoAAME", "firstName": "Ada", "lastName": "Lovelace"})
}

func TestConversationsAndMessages(t *testing.T) {
	me := "urn:li:fsd_profile:ACoAAME"
	r := &routes{replies: map[string]string{
		"/me": meDoc(t),
		"/voyagerMessagingGraphQL/graphql": graphqlDoc(t, "messengerConversationsByCategoryQuery", map[string]any{"metadata": map[string]any{"nextCursor": ""}},
			map[string]any{
				"$type": "com.linkedin.messenger.Conversation", "entityUrn": "urn:li:msg_conversation:(" + me + ",2-abc==)",
				"unreadCount": 2, "lastActivityAt": 1700000000000,
				"*conversationParticipants": []any{"urn:li:msg_messagingParticipant:" + me, "urn:li:msg_messagingParticipant:urn:li:fsd_profile:ACoAAG"},
				"*messages":                 []any{"urn:li:msg_message:1"},
			},
			map[string]any{"$listed": false, "entityUrn": "urn:li:msg_messagingParticipant:" + me, "hostIdentityUrn": me,
				"participantType": map[string]any{"member": map[string]any{"firstName": map[string]any{"text": "Ada"}, "lastName": map[string]any{"text": "Lovelace"}}}},
			map[string]any{"$listed": false, "entityUrn": "urn:li:msg_messagingParticipant:urn:li:fsd_profile:ACoAAG", "hostIdentityUrn": "urn:li:fsd_profile:ACoAAG",
				"participantType": map[string]any{"member": map[string]any{"firstName": map[string]any{"text": "Grace"}, "lastName": map[string]any{"text": "Hopper"}}}},
			map[string]any{"$listed": false, "entityUrn": "urn:li:msg_message:1", "body": map[string]any{"text": "See you!"},
				"*sender": "urn:li:msg_messagingParticipant:urn:li:fsd_profile:ACoAAG"},
		),
	}}
	conversations, err := Conversations(context.Background(), r, "inbox", 10)
	if err != nil || len(conversations) != 1 {
		t.Fatalf("conversations %+v, %v", conversations, err)
	}
	conv := conversations[0]
	if conv.ID != "2-abc==" || len(conv.With) != 1 || conv.With[0] != "Grace Hopper" || conv.Unread != 2 ||
		conv.LastMessage != "See you!" || conv.LastSender != "Grace Hopper" {
		t.Errorf("conversation %+v", conv)
	}
	last := r.last(t)
	if !strings.HasPrefix(last.params[0].Value, "messengerConversations.") || last.params[0].Key != "queryId" ||
		!strings.Contains(last.param("variables"), "category:PRIMARY_INBOX") || !strings.Contains(last.param("variables"), "mailboxUrn:urn%3Ali%3Afsd_profile%3AACoAAME") {
		t.Errorf("inbox request %+v", last.params)
	}

	r.replies["/voyagerMessagingGraphQL/graphql"] = graphqlDoc(t, "messengerMessagesByConversation", nil,
		map[string]any{"entityUrn": "urn:li:msg_message:2", "body": map[string]any{"text": "Second"}, "deliveredAt": 1700000002000},
		map[string]any{"entityUrn": "urn:li:msg_message:1", "body": map[string]any{"text": "First"}, "deliveredAt": 1700000001000},
	)
	messages, err := Messages(context.Background(), r, "2-abc==", 10)
	if err != nil || len(messages) != 2 || messages[0].Text != "First" {
		t.Errorf("messages %+v, %v", messages, err)
	}
	if vars := r.last(t).param("variables"); !strings.Contains(vars, "conversationUrn:urn%3Ali%3Amsg_conversation%3A%28urn%3Ali%3Afsd_profile%3AACoAAME%2C2-abc%3D%3D%29") {
		t.Errorf("messages variables %s", vars)
	}
}

func TestSendMessage(t *testing.T) {
	r := &routes{replies: map[string]string{
		"/me": meDoc(t),
		"/identity/dash/profiles": doc(t, map[string]any{"*elements": []any{"urn:li:fsd_profile:ACoAAG"}},
			map[string]any{"entityUrn": "urn:li:fsd_profile:ACoAAG", "firstName": "Grace", "publicIdentifier": "grace"}),
		"POST /voyagerMessagingDashMessengerMessages": `{"value":{"conversationUrn":"urn:li:msg_conversation:(urn:li:fsd_profile:ACoAAME,2-new==)"}}`,
		"POST /voyagerMessagingDashMessengerConversations/urn:li:msg_conversation:%28urn:li:fsd_profile:ACoAAME%2C2-abc==%29": `{}`,
	}}
	id, err := SendMessage(context.Background(), r, "", ProfileRef{PublicID: "grace"}, "Hello Grace")
	if err != nil || id != "2-new==" {
		t.Fatalf("send to a member: %q, %v", id, err)
	}
	body := r.last(t).body()
	if !strings.Contains(body, `"hostRecipientUrns":["urn:li:fsd_profile:ACoAAG"]`) || !strings.Contains(body, `"text":"Hello Grace"`) ||
		!strings.Contains(body, `"mailboxUrn":"urn:li:fsd_profile:ACoAAME"`) {
		t.Errorf("new conversation payload %s", body)
	}
	if _, err := SendMessage(context.Background(), r, "2-abc==", ProfileRef{}, "Again"); err != nil {
		t.Fatal(err)
	}
	if body := r.last(t).body(); !strings.Contains(body, `"conversationUrn":"urn:li:msg_conversation:(urn:li:fsd_profile:ACoAAME,2-abc==)"`) {
		t.Errorf("reply payload %s", body)
	}
	if err := MarkRead(context.Background(), r, "2-abc=="); err != nil {
		t.Errorf("mark read: %v", err)
	}
	if _, err := SendMessage(context.Background(), r, "2-abc==", ProfileRef{}, "  "); err == nil {
		t.Error("expected an error for an empty message")
	}
	for input, want := range map[string]string{
		"2-abc==": "2-abc==",
		"https://www.linkedin.com/messaging/thread/2-ZTU4MDlh%3D%3D/": "2-ZTU4MDlh==",
	} {
		if got, err := ParseConversation(input); err != nil || got != want {
			t.Errorf("ParseConversation(%q) = %q, %v", input, got, err)
		}
	}
}

func TestSearch(t *testing.T) {
	r := &routes{replies: map[string]string{
		"/graphql": doc(t, map[string]any{"data": map[string]any{"searchDashClustersByAll": map[string]any{
			"elements": []any{map[string]any{"items": []any{
				map[string]any{"item": map[string]any{"*entityResult": "urn:li:fsd_entityResultViewModel:1"}},
				map[string]any{"item": map[string]any{"*entityResult": "urn:li:fsd_entityResultViewModel:2"}},
			}}},
		}}},
			map[string]any{"entityUrn": "urn:li:fsd_entityResultViewModel:1", "title": map[string]any{"text": "Grace Hopper"},
				"primarySubtitle": map[string]any{"text": "Admiral"}, "secondarySubtitle": map[string]any{"text": "New York"},
				"navigationUrl": "https://www.linkedin.com/in/grace?miniProfileUrn=x"},
			map[string]any{"entityUrn": "urn:li:fsd_entityResultViewModel:2", "title": map[string]any{"text": "LinkedIn Member"}},
		),
	}}
	results, err := Search(context.Background(), r, "people", "  grace   hopper ", SearchOptions{Network: []string{"1st", "2nd"}}, 1)
	if err != nil || len(results) != 1 || results[0].Name != "Grace Hopper" || results[0].URL != "https://www.linkedin.com/in/grace" {
		t.Fatalf("results %+v, %v", results, err)
	}
	vars := r.last(t).param("variables")
	if !strings.Contains(vars, "keywords:grace%20hopper") || !strings.Contains(vars, "(key:network,value:List(F|S))") ||
		!strings.Contains(vars, "origin:FACETED_SEARCH") {
		t.Errorf("search variables %s", vars)
	}
	if _, err := Search(context.Background(), r, "posts", "x", SearchOptions{}, 1); err == nil {
		t.Error("posts cannot be searched")
	}
	if _, err := Search(context.Background(), r, "companies", "x", SearchOptions{Network: []string{"1st"}}, 1); err == nil {
		t.Error("the network filter is only for people")
	}
}

func TestCompanyAndFollowing(t *testing.T) {
	r := &routes{replies: map[string]string{
		"/organization/companies": doc(t, map[string]any{"*elements": []any{"urn:li:fs_normalized_company:1234"}},
			map[string]any{
				"$type": "com.linkedin.voyager.organization.Company", "entityUrn": "urn:li:fs_normalized_company:1234",
				"name": "Analytical Engines", "universalName": "engines", "tagline": "Computing since 1837",
				"staffCount": 12, "staffCountRange": map[string]any{"start": 11, "end": 50},
				"headquarter": map[string]any{"city": "London", "country": "GB"}, "*followingInfo": "urn:li:fs_followingInfo:1",
			},
			map[string]any{"entityUrn": "urn:li:fs_followingInfo:1", "followerCount": 900},
		),
		"POST /feed/dash/followingStates/urn:li:fsd_followingState:urn:li:fsd_company:1234": `{}`,
	}}
	company, err := GetCompany(context.Background(), r, "engines")
	if err != nil || company.Name != "Analytical Engines" || company.Size != "11-50 employees" || *company.Followers != 900 ||
		company.Headquarters != "London, GB" || company.URN != "urn:li:fsd_company:1234" {
		t.Fatalf("company %+v, %v", company, err)
	}
	if err := FollowCompany(context.Background(), r, "engines", false); err != nil {
		t.Fatal(err)
	}
	if body := r.last(t).body(); body != `{"patch":{"$set":{"following":false}}}` {
		t.Errorf("unfollow payload %s", body)
	}
}

func TestJobs(t *testing.T) {
	r := &routes{replies: map[string]string{
		"/jobs/jobPostings/4012345678": doc(t, map[string]any{
			"title": "Go engineer", "formattedLocation": "Barcelona", "listedAt": 1700000000000, "applies": 12,
			"description":    map[string]any{"text": "Build CLIs."},
			"applyMethod":    map[string]any{"com.linkedin.voyager.jobs.OffsiteApply": map[string]any{"companyApplyUrl": "https://jobs.example/1"}},
			"companyDetails": map[string]any{"com.linkedin.voyager.deco.jobs.web.shared.WebJobPostingCompany": map[string]any{"*companyResolutionResult": "urn:li:fs_normalized_company:1"}},
			"workplaceTypes": []any{"urn:li:fs_workplaceType:2"},
		}, map[string]any{"entityUrn": "urn:li:fs_normalized_company:1", "name": "Acme"}),
		"/voyagerJobsDashJobCards": doc(t, map[string]any{"*elements": []any{"urn:li:card:1"}},
			map[string]any{"entityUrn": "urn:li:card:1", "jobCard": map[string]any{"*jobPostingCard": "urn:li:fsd_jobPostingCard:(4012345678,JOBS_SEARCH)"}},
			map[string]any{"entityUrn": "urn:li:fsd_jobPostingCard:(4012345678,JOBS_SEARCH)", "jobPostingTitle": "Go engineer",
				"primaryDescription": map[string]any{"text": "Acme"}, "secondaryDescription": map[string]any{"text": "Barcelona (Remote)"},
				"*jobPosting": "urn:li:fsd_jobPosting:4012345678"},
		),
	}}
	job, err := GetJob(context.Background(), r, "4012345678")
	if err != nil || job.Title != "Go engineer" || job.Company != "Acme" || job.ApplyURL != "https://jobs.example/1" ||
		job.Workplace != "remote" || *job.Applicants != 12 {
		t.Fatalf("job %+v, %v", job, err)
	}
	jobs, err := SearchJobs(context.Background(), r, JobSearch{Keywords: "go", Location: "Barcelona, Spain", Workplace: []string{"remote"}, Posted: "week"}, 5)
	if err != nil || len(jobs) != 1 || jobs[0].ID != "4012345678" || jobs[0].Company != "Acme" {
		t.Fatalf("jobs %+v, %v", jobs, err)
	}
	query := r.last(t).param("query")
	for _, want := range []string{"keywords:go", "location:Barcelona%2C%20Spain", "workplaceType:List(2)", "timePostedRange:List(r604800)"} {
		if !strings.Contains(query, want) {
			t.Errorf("job search query %s lacks %s", query, want)
		}
	}
	if _, err := SearchJobs(context.Background(), r, JobSearch{Posted: "year"}, 5); err == nil {
		t.Error("expected an error for an unknown period")
	}
}

func TestStaleQueriesTryTheNextVersionThenExplain(t *testing.T) {
	gone := &goneFirst{routes: &routes{replies: map[string]string{"/graphql": graphqlDoc(t, "socialDashReactionsByReactionType", nil)}}}
	if _, err := Reactions(context.Background(), gone, "urn:li:activity:1", 1); err != nil {
		t.Fatalf("the second known version should answer: %v", err)
	}
	if first, second := gone.calls[0].param("queryId"), gone.calls[1].param("queryId"); first == second {
		t.Errorf("the same version was tried twice: %s", first)
	}

	SetLearnedQueries([]LearnedQuery{{Name: "voyagerSocialDashReactions", Variables: []string{"count", "start", "threadUrn"}, Hash: "learned"}})
	defer SetLearnedQueries(nil)
	r := &routes{replies: map[string]string{"/graphql": graphqlDoc(t, "socialDashReactionsByReactionType", nil)}}
	if _, err := Reactions(context.Background(), r, "urn:li:activity:1", 1); err != nil || r.calls[0].param("queryId") != "voyagerSocialDashReactions.learned" {
		t.Errorf("learned version not tried first: %+v, %v", r.calls, err)
	}
	if err := React(context.Background(), &routes{replies: map[string]string{"POST /graphql": `{}`}}, "urn:li:activity:1", "like"); err != nil {
		t.Errorf("react: %v", err)
	}
}

// reposted answers like LinkedIn for a repost: its own thread is empty and
// the post's SocialDetail names the thread of the original post.
type reposted struct {
	*routes
	original string
}

func (r reposted) Get(ctx context.Context, path string, params voyager.Params) ([]byte, error) {
	vars := call{params: params}.param("variables")
	if strings.Contains(vars, "updateUrn") || strings.Contains(vars, restliValue(r.original)) {
		return r.routes.Get(ctx, path, params)
	}
	r.routes.calls = append(r.routes.calls, call{"GET", path, params, nil})
	return []byte(`{"data":{"data":{}},"included":[]}`), nil
}

func TestRepostsReadTheThreadOfTheOriginalPost(t *testing.T) {
	repost, original := "urn:li:activity:2", "urn:li:activity:1"
	detail := "urn:li:fsd_socialDetail:(" + original + "," + repost + ",urn:li:highlightedReply:-)"
	update := doc(t, map[string]any{},
		map[string]any{"$type": "com.linkedin.voyager.dash.feed.Update", "entityUrn": "urn:li:fsd_update:(" + repost + ",MESSAGING_RESHARE,EMPTY,DEFAULT,false)", "*socialDetail": detail},
		map[string]any{"$type": "com.linkedin.voyager.dash.social.SocialDetail", "entityUrn": detail, "threadUrn": original})
	r := reposted{original: original, routes: &routes{replies: map[string]string{
		"/graphql#voyagerFeedDashUpdates": update,
		"/graphql#voyagerSocialDashReactions": graphqlDoc(t, "socialDashReactionsByReactionType", nil,
			map[string]any{"entityUrn": "urn:li:fsd_reaction:1", "reactionType": "LIKE", "reactorLockup": map[string]any{"title": map[string]any{"text": "Alan Turing"}}}),
		"/graphql#voyagerSocialDashComments": graphqlDoc(t, "socialDashCommentsBySocialDetail", nil,
			map[string]any{"$type": "com.linkedin.voyager.dash.social.Comment", "entityUrn": "urn:li:fsd_comment:(1," + original + ")", "commentary": map[string]any{"text": "Nice"}}),
	}}}
	reactions, err := Reactions(context.Background(), r, repost, 5)
	if err != nil || len(reactions) != 1 {
		t.Fatalf("reactions of a repost %+v, %v", reactions, err)
	}
	if vars := r.last(t).param("variables"); !strings.Contains(vars, restliValue(original)) {
		t.Errorf("reactions read %s, not the original thread", vars)
	}
	comments, err := Comments(context.Background(), r, repost, 5)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments of a repost %+v, %v", comments, err)
	}
	if vars := r.last(t).param("variables"); !strings.Contains(vars, restliValue(detail)) {
		t.Errorf("comments read %s, not the post's SocialDetail", vars)
	}
}

func TestPostsWithTheirOwnThreadCostOneRequest(t *testing.T) {
	r := &routes{replies: map[string]string{"/graphql": graphqlDoc(t, "socialDashReactionsByReactionType", nil,
		map[string]any{"entityUrn": "urn:li:fsd_reaction:1", "reactionType": "LIKE", "reactorLockup": map[string]any{"title": map[string]any{"text": "Alan Turing"}}})}}
	if _, err := Reactions(context.Background(), r, "urn:li:activity:1", 5); err != nil || len(r.calls) != 1 {
		t.Errorf("%d requests, %v", len(r.calls), err)
	}
}

func TestUnreadCounts(t *testing.T) {
	r := &routes{replies: map[string]string{
		"/voyagerNotificationsDashBadgingItemCounts": `{"data":{"elements":[{"count":4,"badgingItem":"NOTIFICATIONS"},{"count":2,"badgingItem":"MESSAGING"},` +
			`{"count":1,"badgingItem":"MY_NETWORK"},{"count":5,"badgingItem":"NURTURE"}]},"included":[]}`,
		"/me": `{"data":{"*miniProfile":"urn:li:fs_miniProfile:ACoAAA"},"included":[{"entityUrn":"urn:li:fs_miniProfile:ACoAAA","firstName":"Ada","publicIdentifier":"ada"}]}`,
		"/voyagerMessagingGraphQL/graphql#messengerMailboxCounts": `{"data":{"messengerMailboxCountsByMailbox":{"elements":[` +
			`{"category":"PRIMARY_INBOX","unreadConversationCount":2},{"category":"MESSAGE_REQUEST_PENDING","unreadConversationCount":1},{"category":"INBOX","unreadConversationCount":3}]}}}`,
	}}
	u, err := UnreadCounts(context.Background(), r, false)
	if err != nil || u.Notifications != 4 || u.Messages != 2 || u.Network != 1 || u.Other["nurture"] != 5 || u.Folders != nil {
		t.Fatalf("unread %+v, %v", u, err)
	}
	if len(r.calls) != 1 {
		t.Errorf("badges took %d requests", len(r.calls))
	}
	u, err = UnreadCounts(context.Background(), r, true)
	if err != nil || u.Folders["inbox"] != 2 || u.Folders["requests"] != 1 || len(u.Folders) != 2 {
		t.Fatalf("folders %+v, %v", u.Folders, err)
	}
	if vars := r.last(t).param("variables"); vars != "(mailboxUrn:urn%3Ali%3Afsd_profile%3AACoAAA)" {
		t.Errorf("mailbox variables %s", vars)
	}
}
