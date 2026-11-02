package api

import (
	"context"
	"strings"
	"testing"
)

func myItemsPage(t *testing.T, rows ...map[string]any) string {
	t.Helper()
	var items []any
	for _, row := range rows {
		items = append(items, map[string]any{"item": map[string]any{"*entityResult": row["entityUrn"]}})
	}
	return doc(t, map[string]any{"data": map[string]any{"searchDashClustersByAll": map[string]any{
		"elements": []any{map[string]any{"items": items}},
	}}}, rows...)
}

func TestSavedPostsAndTrackedJobs(t *testing.T) {
	r := &routes{replies: map[string]string{"/graphql": myItemsPage(t,
		map[string]any{"entityUrn": "urn:li:fsd_entityResultViewModel:(urn:li:activity:42,SEARCH_MY_ITEMS_SAVED_POSTS,DEFAULT)",
			"title": map[string]any{"text": "Grace Hopper"}, "primarySubtitle": map[string]any{"text": "Admiral"},
			"secondarySubtitle": map[string]any{"text": "5d •"}, "summary": map[string]any{"text": "On compilers"}},
	)}}
	posts, err := SavedPosts(context.Background(), r, 10)
	if err != nil || len(posts) != 1 {
		t.Fatalf("saved posts %+v, %v", posts, err)
	}
	if p := posts[0]; p.URN != "urn:li:activity:42" || p.Author != "Grace Hopper" || p.Age != "5d" || p.Text != "On compilers" {
		t.Errorf("saved post %+v", p)
	}
	vars := r.last(t).param("variables")
	if vars != "(start:0,query:(flagshipSearchIntent:SEARCH_MY_ITEMS_SAVED_POSTS))" || strings.Contains(vars, "origin") {
		t.Errorf("saved posts variables %s", vars)
	}

	r.replies["/graphql"] = myItemsPage(t,
		map[string]any{"entityUrn": "urn:li:fsd_entityResultViewModel:(urn:li:fsd_jobPosting:4012345678,SEARCH_MY_ITEMS_JOB_SEEKER,DEFAULT)",
			"title": map[string]any{"text": "Go engineer"}, "primarySubtitle": map[string]any{"text": "Acme"}, "secondarySubtitle": map[string]any{"text": "Barcelona (Remote)"}},
	)
	jobs, err := TrackedJobs(context.Background(), r, "applied", 10)
	if err != nil || len(jobs) != 1 || jobs[0].ID != "4012345678" || jobs[0].Company != "Acme" {
		t.Fatalf("tracked jobs %+v, %v", jobs, err)
	}
	if vars := r.last(t).param("variables"); !strings.Contains(vars, "(key:cardType,value:List(APPLIED))") {
		t.Errorf("tracked jobs variables %s", vars)
	}
	if _, err := TrackedJobs(context.Background(), r, "rejected", 10); err == nil {
		t.Error("expected an error for an unknown stage")
	}
}
