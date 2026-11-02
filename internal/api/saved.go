package api

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/normalized"
)

// The "My items" pages (saved posts, the job tracker) are the search query
// with a My-items intent, no keywords and no origin.
var savedItemsQuery = Query{
	Name: "voyagerSearchDashClusters", Vars: []string{"start", "query"}, Optional: []string{"paginationToken"},
	Hashes: searchQuery.Hashes,
}

// JobStages are the columns of the job tracker.
var JobStages = map[string]string{
	"saved": "SAVED", "in-progress": "IN_PROGRESS", "applied": "APPLIED", "interview": "INTERVIEW", "archived": "ARCHIVED",
}

// JobStageNames lists the job tracker stages in the website's order.
var JobStageNames = []string{"saved", "in-progress", "applied", "interview", "archived"}

// SavedPost is a post you saved.
type SavedPost struct {
	URN            string `json:"urn"`
	URL            string `json:"url"`
	Author         string `json:"author,omitempty"`
	AuthorHeadline string `json:"author_headline,omitempty"`
	Age            string `json:"age,omitempty"`
	Text           string `json:"text,omitempty"`
}

var innerURN = regexp.MustCompile(`\((urn:li:[A-Za-z_]+:[^,)]+)`)

// SavedPosts returns the posts you saved, most recently saved first.
func SavedPosts(ctx context.Context, c Client, limit int) ([]SavedPost, error) {
	if err := checkLimit(limit, MaxListItems); err != nil {
		return nil, err
	}
	return paginate(limit, func(start int) ([]SavedPost, int, error) {
		raw, err := savedItemsQuery.Get(ctx, c, fmt.Sprintf("(start:%d,query:(flagshipSearchIntent:SEARCH_MY_ITEMS_SAVED_POSTS))", start))
		if err != nil {
			return nil, 0, err
		}
		rows, n := searchRows(raw)
		var posts []SavedPost
		for _, row := range rows {
			urn := innerOf(row)
			if !postRef.MatchString(urn) {
				continue
			}
			posts = append(posts, SavedPost{
				URN:            urn,
				URL:            "https://www.linkedin.com/feed/update/" + urn + "/",
				Author:         normalized.Text(row["title"]),
				AuthorHeadline: normalized.Text(row["primarySubtitle"]),
				Age:            strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(normalized.Text(row["secondarySubtitle"])), "•")),
				Text:           normalized.Text(row["summary"]),
			})
		}
		return posts, pageEnd(nil, start, n, ListPageSize), nil
	})
}

// TrackedJobs returns the jobs of one stage of your job tracker.
func TrackedJobs(ctx context.Context, c Client, stage string, limit int) ([]Job, error) {
	if err := checkLimit(limit, MaxListItems); err != nil {
		return nil, err
	}
	cardType, ok := JobStages[stage]
	if !ok {
		return nil, errs.New(errs.Usage, "unknown stage %q; use %s", stage, strings.Join(JobStageNames, ", "))
	}
	return paginate(limit, func(start int) ([]Job, int, error) {
		raw, err := savedItemsQuery.Get(ctx, c, fmt.Sprintf(
			"(start:%d,query:(flagshipSearchIntent:SEARCH_MY_ITEMS_JOB_SEEKER,queryParameters:List((key:cardType,value:List(%s)))))", start, cardType))
		if err != nil {
			return nil, 0, err
		}
		rows, n := searchRows(raw)
		var jobs []Job
		for _, row := range rows {
			m := jobPostingID.FindStringSubmatch(innerOf(row))
			if m == nil {
				continue
			}
			jobs = append(jobs, Job{
				ID:       m[1],
				Title:    normalized.Text(row["title"]),
				Company:  normalized.Text(row["primarySubtitle"]),
				Location: normalized.Text(row["secondarySubtitle"]),
				URL:      "https://www.linkedin.com/jobs/view/" + m[1] + "/",
			})
		}
		return jobs, pageEnd(nil, start, n, ListPageSize), nil
	})
}

// innerOf is the URN wrapped in a search row's URN, such as the post in
// urn:li:fsd_entityResultViewModel:(urn:li:activity:1,SEARCH_MY_ITEMS_SAVED_POSTS,DEFAULT).
func innerOf(row normalized.Object) string {
	for _, key := range []string{"entityUrn", "trackingUrn"} {
		if m := innerURN.FindStringSubmatch(normalized.String(row[key])); m != nil {
			return m[1]
		}
	}
	return normalized.String(row["trackingUrn"])
}
