package api

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/normalized"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

const (
	// JobDecoration selects a full job posting.
	JobDecoration = "com.linkedin.voyager.deco.jobs.web.shared.WebFullJobPosting-65"
	// JobSearchDecoration selects job search cards.
	JobSearchDecoration = "com.linkedin.voyager.dash.deco.jobs.search.JobSearchCardsCollection-220"
	// JobPageSize is the page size of the website's job search.
	JobPageSize = 25
)

// Job is a job posting.
type Job struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Company     string `json:"company,omitempty"`
	Location    string `json:"location,omitempty"`
	Workplace   string `json:"workplace,omitempty"`
	Listed      string `json:"listed,omitempty"`
	Applicants  *int   `json:"applicants"`
	Description string `json:"description,omitempty"`
	ApplyURL    string `json:"apply_url,omitempty"`
	URL         string `json:"url"`
}

// JobSearch narrows a job search.
type JobSearch struct {
	Keywords string
	Location string
	// Workplace is "onsite", "remote" or "hybrid".
	Workplace []string
	// Posted is "day", "week" or "month".
	Posted string
}

var (
	jobWorkplaces  = map[string]string{"onsite": "1", "remote": "2", "hybrid": "3"}
	jobPosted      = map[string]string{"day": "r86400", "week": "r604800", "month": "r2592000"}
	workplaceName  = map[string]string{"1": "on-site", "2": "remote", "3": "hybrid"}
	jobPostingID   = regexp.MustCompile(`jobPosting:(\d+)`)
	trailingNumber = regexp.MustCompile(`\d+$`)
)

// GetJob returns a job posting by id.
func GetJob(ctx context.Context, c Client, id string) (Job, error) {
	raw, err := c.Get(ctx, "/jobs/jobPostings/"+id, voyager.Params{{Key: "decorationId", Value: JobDecoration}})
	if err != nil {
		return Job{}, err
	}
	return ParseJobPosting(raw, id)
}

// ParseJobPosting reads a job posting response.
func ParseJobPosting(raw []byte, id string) (Job, error) {
	doc := normalized.Parse(raw)
	posting := doc.Data
	if normalized.String(posting["title"]) == "" {
		for _, entity := range append(doc.OfType(".JobPosting"), doc.OfType("WebFullJobPosting")...) {
			if normalized.String(entity["title"]) != "" {
				posting = entity
				break
			}
		}
	}
	if normalized.String(posting["title"]) == "" {
		return Job{}, errs.New(errs.API, "LinkedIn did not return job %s; it may be closed or removed", id)
	}
	job := Job{
		ID:          id,
		Title:       normalized.String(posting["title"]),
		Location:    normalized.String(posting["formattedLocation"]),
		Listed:      millisDate(firstOf(posting, "listedAt", "originalListedAt")),
		Applicants:  intPtr(posting["applies"]),
		Description: normalized.Text(posting["description"]),
		URL:         "https://www.linkedin.com/jobs/view/" + id + "/",
	}
	// applyMethod and companyDetails are unions keyed by type name.
	for _, variant := range unionValues(posting["applyMethod"]) {
		if u := normalized.String(variant["companyApplyUrl"]); u != "" {
			job.ApplyURL = u
		}
	}
	for _, variant := range unionValues(posting["companyDetails"]) {
		if company := doc.Ref(variant, "companyResolutionResult"); company != nil {
			job.Company = normalized.String(company["name"])
		}
		if job.Company == "" {
			job.Company = normalized.String(variant["companyName"])
		}
	}
	var workplaces []string
	for _, w := range stringList(posting["workplaceTypes"]) {
		if m := trailingNumber.FindString(w); workplaceName[m] != "" {
			workplaces = append(workplaces, workplaceName[m])
		}
	}
	job.Workplace = strings.Join(workplaces, ", ")
	return job, nil
}

// SearchJobs searches job postings, most relevant first.
func SearchJobs(ctx context.Context, c Client, search JobSearch, limit int) ([]Job, error) {
	if err := checkLimit(limit, MaxListItems); err != nil {
		return nil, err
	}
	query := []string{"origin:JOB_SEARCH_PAGE_JOB_FILTER"}
	if k := strings.Join(strings.Fields(search.Keywords), " "); k != "" {
		query = append(query, "keywords:"+restliValue(k))
	}
	if search.Location != "" {
		query = append(query, "locationUnion:(seoLocation:(location:"+restliValue(search.Location)+"))")
	}
	var filters []string
	if len(search.Workplace) > 0 {
		var ids []string
		for _, w := range search.Workplace {
			id, ok := jobWorkplaces[w]
			if !ok {
				return nil, errs.New(errs.Usage, "unknown workplace %q; use onsite, remote or hybrid", w)
			}
			ids = append(ids, id)
		}
		filters = append(filters, "workplaceType:List("+strings.Join(ids, ",")+")")
	}
	if search.Posted != "" {
		r, ok := jobPosted[search.Posted]
		if !ok {
			return nil, errs.New(errs.Usage, "unknown period %q; use day, week or month", search.Posted)
		}
		filters = append(filters, "timePostedRange:List("+r+")")
	}
	if len(filters) > 0 {
		query = append(query, "selectedFilters:("+strings.Join(filters, ",")+")")
	}
	query = append(query, "spellCorrectionEnabled:true")
	return paginate(limit, func(start int) ([]Job, int, error) {
		raw, err := c.Get(ctx, "/voyagerJobsDashJobCards", voyager.Params{
			{Key: "decorationId", Value: JobSearchDecoration},
			{Key: "count", Value: strconv.Itoa(JobPageSize)},
			{Key: "q", Value: "jobSearch"},
			{Key: "query", Value: "(" + strings.Join(query, ",") + ")"},
			{Key: "start", Value: strconv.Itoa(start)},
		})
		if err != nil {
			return nil, 0, err
		}
		found, n := ParseJobSearch(raw)
		return found, pageEnd(nil, start, n, JobPageSize), nil
	})
}

// ParseJobSearch reads one page of job search results and how many cards it had.
func ParseJobSearch(raw []byte) ([]Job, int) {
	doc := normalized.Parse(raw)
	items, n, coll := collection(doc)
	var cards []normalized.Object
	for _, item := range items {
		card := doc.Ref(item, "jobCard")
		if inner := doc.Ref(card, "jobPostingCard"); inner != nil {
			card = inner
		}
		if card == nil {
			card = doc.Ref(item, "jobPostingCard")
		}
		if card != nil {
			cards = append(cards, card)
		}
	}
	if coll == nil || len(cards) == 0 {
		cards = doc.OfType("JobPostingCard")
		if coll == nil {
			n = len(cards)
		}
	}
	var out []Job
	seen := map[string]bool{}
	for _, card := range cards {
		id := ""
		for _, candidate := range []string{normalized.String(card["*jobPosting"]), normalized.String(card["jobPostingUrn"]), normalized.String(card["entityUrn"])} {
			if m := jobPostingID.FindStringSubmatch(candidate); m != nil {
				id = m[1]
				break
			}
		}
		title := firstNonEmpty(normalized.Text(card["jobPostingTitle"]), normalized.Text(card["title"]))
		if id == "" || title == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Job{
			ID:       id,
			Title:    title,
			Company:  normalized.Text(card["primaryDescription"]),
			Location: normalized.Text(card["secondaryDescription"]),
			URL:      fmt.Sprintf("https://www.linkedin.com/jobs/view/%s/", id),
		})
	}
	return out, n
}

// unionValues returns the variants of a Rest.li union such as
// {"com.linkedin.voyager.jobs.OffsiteApply": {...}}.
func unionValues(value any) []normalized.Object {
	obj, _ := value.(normalized.Object)
	var out []normalized.Object
	for _, v := range obj {
		if variant, ok := v.(normalized.Object); ok {
			out = append(out, variant)
		}
	}
	return out
}

func stringList(value any) []string {
	list, _ := value.([]any)
	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
