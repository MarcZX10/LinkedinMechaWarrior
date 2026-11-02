package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/normalized"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

// CompanyDecoration selects the full company page.
const CompanyDecoration = "com.linkedin.voyager.deco.organization.web.WebFullCompanyMain-12"

// Company is a company or school page.
type Company struct {
	Name         string   `json:"name"`
	Tagline      string   `json:"tagline,omitempty"`
	Description  string   `json:"description,omitempty"`
	Industries   []string `json:"industries,omitempty"`
	Website      string   `json:"website,omitempty"`
	Headquarters string   `json:"headquarters,omitempty"`
	Size         string   `json:"size,omitempty"`
	Employees    *int     `json:"employees_on_linkedin"`
	Followers    *int     `json:"followers"`
	Founded      *int     `json:"founded"`
	URL          string   `json:"url"`
	URN          string   `json:"urn,omitempty"`
}

// GetCompany returns a company or school page by its universal name, the
// "anthropic" in linkedin.com/company/anthropic.
func GetCompany(ctx context.Context, c Client, name string) (Company, error) {
	raw, err := c.Get(ctx, "/organization/companies", voyager.Params{
		{Key: "decorationId", Value: CompanyDecoration},
		{Key: "q", Value: "universalName"},
		{Key: "universalName", Value: name},
	})
	if err != nil {
		return Company{}, err
	}
	return ParseCompanyPage(raw, name)
}

// ParseCompanyPage reads a company page response.
func ParseCompanyPage(raw []byte, name string) (Company, error) {
	doc := normalized.Parse(raw)
	var page normalized.Object
	items, _, _ := collection(doc)
	for _, entity := range append(items, doc.OfType(".Company")...) {
		if normalized.String(entity["name"]) == "" {
			continue
		}
		if strings.EqualFold(normalized.String(entity["universalName"]), name) {
			page = entity
			break
		}
		if page == nil {
			page = entity
		}
	}
	if page == nil {
		return Company{}, errs.New(errs.Usage, "no company or school called %q on LinkedIn", name)
	}
	company := Company{
		Name:        normalized.String(page["name"]),
		Tagline:     normalized.Text(page["tagline"]),
		Description: normalized.Text(page["description"]),
		Website:     firstNonEmpty(normalized.String(page["companyPageUrl"]), normalized.String(page["websiteUrl"])),
		Employees:   intPtr(page["staffCount"]),
		URL:         "https://www.linkedin.com/company/" + firstNonEmpty(normalized.String(page["universalName"]), name) + "/",
		URN:         companyURN(normalized.String(page["entityUrn"])),
	}
	if hq, ok := page["headquarter"].(normalized.Object); ok {
		company.Headquarters = joinNonEmpty(", ", normalized.String(hq["city"]), normalized.String(hq["geographicArea"]), normalized.String(hq["country"]))
	}
	if size, ok := page["staffCountRange"].(normalized.Object); ok {
		start, hasStart := normalized.Int(size["start"])
		end, hasEnd := normalized.Int(size["end"])
		switch {
		case hasStart && hasEnd:
			company.Size = fmt.Sprintf("%d-%d employees", start, end)
		case hasStart:
			company.Size = fmt.Sprintf("%d+ employees", start)
		}
	}
	if year, ok := normalized.Dig(page, "foundedOn", "year").(float64); ok {
		y := int(year)
		company.Founded = &y
	}
	if following := doc.Ref(page, "followingInfo"); following != nil {
		company.Followers = intPtr(following["followerCount"])
	}
	for _, industry := range doc.Refs(page, "companyIndustries") {
		if name := normalized.String(industry["localizedName"]); name != "" {
			company.Industries = append(company.Industries, name)
		}
	}
	return company, nil
}

// CompanyID returns the number LinkedIn knows a company or school page by
// (one request).
func CompanyID(ctx context.Context, c Client, name string) (string, error) {
	company, err := GetCompany(ctx, c, name)
	if err != nil {
		return "", err
	}
	id := strings.TrimPrefix(company.URN, "urn:li:fsd_company:")
	if id == "" || strings.Contains(id, ":") {
		return "", errs.New(errs.API, "LinkedIn did not say which page %q is; inspect it with `lmw company show %s --json`", name, name)
	}
	return id, nil
}

// CompanyPosts returns the latest posts of a company or school page, as
// its Posts tab shows them, from the page's id (see CompanyID).
func CompanyPosts(ctx context.Context, c Client, id string, limit int) ([]FeedPost, error) {
	if limit < 1 || limit > MaxFeedPosts {
		return nil, errs.New(errs.Usage, "--limit must be between 1 and %d", MaxFeedPosts)
	}
	page := restliValue("urn:li:fsd_organizationalPage:" + id)
	return paginatePosts(limit, func(start int) ([]byte, error) {
		return organizationUpdatesQuery.Get(ctx, c, fmt.Sprintf("(count:%d,start:%d,moduleKey:ORGANIZATION_MEMBER_FEED_DESKTOP,organizationalPageUrn:%s)", FeedPageSize, start, page))
	})
}

// companyURN turns the company URN of a company page into the dash form
// that following uses: urn:li:fsd_company:ID.
func companyURN(urn string) string {
	if i := strings.LastIndex(urn, ":"); i >= 0 && strings.Contains(urn, "company") {
		return "urn:li:fsd_company:" + urn[i+1:]
	}
	return urn
}

func joinNonEmpty(separator string, values ...string) string {
	var kept []string
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			kept = append(kept, v)
		}
	}
	return strings.Join(kept, separator)
}

// SetFollowing follows or unfollows a member or company, given its dash URN
// (urn:li:fsd_profile:... or urn:li:fsd_company:...).
func SetFollowing(ctx context.Context, c Client, urn string, follow bool) error {
	_, err := c.Post(ctx, "/feed/dash/followingStates/urn:li:fsd_followingState:"+urn, nil,
		map[string]any{"patch": map[string]any{"$set": map[string]any{"following": follow}}})
	return err
}

// FollowMember follows or unfollows a member.
func FollowMember(ctx context.Context, c Client, ref ProfileRef, follow bool) error {
	urn, err := MemberURN(ctx, c, ref)
	if err != nil {
		return err
	}
	return SetFollowing(ctx, c, urn, follow)
}

// FollowCompany follows or unfollows a company or school.
func FollowCompany(ctx context.Context, c Client, name string, follow bool) error {
	company, err := GetCompany(ctx, c, name)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(company.URN, "urn:li:fsd_company:") {
		return errs.New(errs.API, "LinkedIn did not return the id of %s", name)
	}
	return SetFollowing(ctx, c, company.URN, follow)
}
