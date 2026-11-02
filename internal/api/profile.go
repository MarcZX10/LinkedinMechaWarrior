package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/normalized"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

// Decorations of the dash profiles endpoint the website uses. The first
// has every section (experience, education, skills); the second only the
// profile itself; the third is the small top card. LinkedIn bumps the
// numbers now and then, which makes the old ones answer 400.
const (
	ProfileDecoration        = "com.linkedin.voyager.dash.deco.identity.profile.FullProfileWithEntities-93"
	ProfileShortDecoration   = "com.linkedin.voyager.dash.deco.identity.profile.FullProfile-76"
	ProfileTopCardDecoration = "com.linkedin.voyager.dash.deco.identity.profile.WebTopCardCore-19"
)

// dashProfile reads /identity/dash/profiles with the first decoration
// LinkedIn accepts.
func dashProfile(ctx context.Context, c Client, ref ProfileRef, decorations ...string) ([]byte, error) {
	var raw []byte
	var err error
	for _, decoration := range decorations {
		raw, err = c.Get(ctx, "/identity/dash/profiles", voyager.Params{
			{Key: "q", Value: "memberIdentity"},
			{Key: "memberIdentity", Value: ref.String()},
			{Key: "decorationId", Value: decoration},
		})
		if !endpointGone(err) {
			break
		}
	}
	return raw, err
}

// GetProfile returns a member's profile.
func GetProfile(ctx context.Context, c Client, ref ProfileRef) (ProfileDetails, error) {
	raw, err := dashProfile(ctx, c, ref, ProfileDecoration, ProfileShortDecoration)
	if err != nil {
		return ProfileDetails{}, err
	}
	return ParseProfileView(raw, ref)
}

// endpointGone reports whether LinkedIn rejected an endpoint itself (it was
// removed or changed), as opposed to the session or the rate limit.
func endpointGone(err error) bool {
	var httpErr *errs.HTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	switch httpErr.Status {
	case 400, 404, 410, 422:
		return true
	}
	return false
}

// ProfileDetails is a member profile as shown on their profile page.
type ProfileDetails struct {
	Profile
	Location   string      `json:"location,omitempty"`
	Industry   string      `json:"industry,omitempty"`
	About      string      `json:"about,omitempty"`
	Experience []Position  `json:"experience"`
	Education  []Education `json:"education"`
	Skills     []string    `json:"skills"`
}

// Position is one job in a profile's experience.
type Position struct {
	Title       string `json:"title,omitempty"`
	Company     string `json:"company,omitempty"`
	Location    string `json:"location,omitempty"`
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
	Description string `json:"description,omitempty"`
}

// Education is one school in a profile.
type Education struct {
	School string `json:"school,omitempty"`
	Degree string `json:"degree,omitempty"`
	Field  string `json:"field,omitempty"`
	Start  string `json:"start,omitempty"`
	End    string `json:"end,omitempty"`
}

// ParseProfileView reads a profile response. Both the classic profileView
// and the dash profiles endpoints put every entity in "included", so they
// are found by type; ref picks the right profile when several are included.
func ParseProfileView(raw []byte, ref ProfileRef) (ProfileDetails, error) {
	doc := normalized.Parse(raw)
	main := pickProfile(doc, ref)
	if main == nil {
		return ProfileDetails{}, errs.New(errs.API, "no profile found in LinkedIn's answer for %s. Inspect it with `lmw api get`", ref)
	}
	p := ProfileDetails{
		Profile: Profile{
			URN:       firstNonEmpty(normalized.String(main["entityUrn"]), normalized.String(main["dashEntityUrn"])),
			PublicID:  normalized.String(main["publicIdentifier"]),
			FirstName: normalized.String(main["firstName"]),
			LastName:  normalized.String(main["lastName"]),
			Headline:  firstNonEmpty(normalized.Text(main["headline"]), normalized.Text(main["occupation"])),
		},
		Location: firstNonEmpty(normalized.String(main["locationName"]), normalized.String(main["geoLocationName"]),
			normalized.Text(normalized.Dig(main, "geoLocation", "geo", "defaultLocalizedName")),
			normalized.Text(normalized.Dig(doc.Ref(doc.Ref(main, "geoLocation"), "geo"), "defaultLocalizedName"))),
		Industry:   firstNonEmpty(normalized.String(main["industryName"]), normalized.Text(normalized.Dig(doc.Ref(main, "industry"), "name"))),
		About:      firstNonEmpty(normalized.Text(main["summary"]), normalized.Text(main["multiLocaleSummary"])),
		Experience: []Position{},
		Education:  []Education{},
		Skills:     []string{},
	}
	p.Profile = finishProfile(p.Profile, ref)
	if p.URN == "" || strings.HasPrefix(p.URN, "urn:li:fs_profile:") {
		// The member URN that other endpoints expect.
		if id := memberIDFromURN(p.URN); id != "" {
			p.URN = "urn:li:fsd_profile:" + id
		}
	}

	for _, pos := range doc.OfType(".Position") {
		p.Experience = append(p.Experience, Position{
			Title:       normalized.Text(pos["title"]),
			Company:     firstNonEmpty(normalized.Text(pos["companyName"]), normalized.Text(normalized.Dig(doc.Ref(pos, "company"), "name"))),
			Location:    firstNonEmpty(normalized.Text(pos["locationName"]), normalized.Text(pos["geoLocationName"])),
			Start:       yearMonth(dateIn(pos, "start")),
			End:         yearMonth(dateIn(pos, "end")),
			Description: normalized.Text(pos["description"]),
		})
	}
	for _, edu := range doc.OfType(".Education") {
		p.Education = append(p.Education, Education{
			School: firstNonEmpty(normalized.Text(edu["schoolName"]), normalized.Text(normalized.Dig(doc.Ref(edu, "school"), "name"))),
			Degree: normalized.Text(edu["degreeName"]),
			Field:  normalized.Text(edu["fieldOfStudy"]),
			Start:  yearMonth(dateIn(edu, "start")),
			End:    yearMonth(dateIn(edu, "end")),
		})
	}
	seen := map[string]bool{}
	for _, skill := range doc.OfType(".Skill") {
		if name := normalized.Text(skill["name"]); name != "" && !seen[name] {
			seen[name] = true
			p.Skills = append(p.Skills, name)
		}
	}
	return p, nil
}

func pickProfile(doc *normalized.Doc, ref ProfileRef) normalized.Object {
	if urns, ok := doc.Data["*elements"].([]any); ok && len(urns) > 0 {
		if entity := doc.Get(urns[0]); entity != nil {
			return entity
		}
	}
	if entity := doc.Ref(doc.Data, "profile"); entity != nil {
		return entity
	}
	var first normalized.Object
	for _, entity := range append(doc.OfType(".Profile"), doc.OfType(".MiniProfile")...) {
		if normalized.String(entity["firstName"]) == "" {
			continue
		}
		public := normalized.String(entity["publicIdentifier"])
		id := memberIDFromURN(firstNonEmpty(normalized.String(entity["entityUrn"]), normalized.String(entity["dashEntityUrn"])))
		if (ref.PublicID != "" && strings.EqualFold(public, ref.PublicID)) || (ref.MemberID != "" && id == ref.MemberID) {
			return entity
		}
		if first == nil {
			first = entity
		}
	}
	return first
}

// finishProfile fills the derived fields of a profile.
func finishProfile(p Profile, ref ProfileRef) Profile {
	if p.PublicID == "" {
		p.PublicID = ref.PublicID
	}
	p.Name = strings.TrimSpace(p.FirstName + " " + p.LastName)
	if p.Name == "" {
		p.Name = "(unknown)"
	}
	if p.PublicID != "" {
		p.URL = "https://www.linkedin.com/in/" + p.PublicID + "/"
	}
	return p
}

// memberIDFromURN returns the "ACoAA..." id inside a profile URN.
func memberIDFromURN(urn string) string {
	if m := profileURN.FindStringSubmatch(urn); m != nil {
		return m[1]
	}
	return ""
}

// dateIn finds the start or end date of a position or school in the
// classic (timePeriod.startDate) or dash (dateRange.start) shape.
func dateIn(entity normalized.Object, which string) normalized.Object {
	for _, path := range [][]string{{"dateRange", which}, {"timePeriod", which + "Date"}} {
		if date, ok := normalized.Dig(entity, path...).(normalized.Object); ok {
			return date
		}
	}
	return nil
}

func yearMonth(date normalized.Object) string {
	year, ok := normalized.Int(date["year"])
	if !ok {
		return ""
	}
	if month, ok := normalized.Int(date["month"]); ok {
		return fmt.Sprintf("%04d-%02d", year, month)
	}
	return fmt.Sprintf("%04d", year)
}

// MemberURN returns the urn:li:fsd_profile URN that endpoints about a member
// expect, looking it up when only the public identifier is known.
func MemberURN(ctx context.Context, c Client, ref ProfileRef) (string, error) {
	if ref.MemberID != "" {
		return "urn:li:fsd_profile:" + ref.MemberID, nil
	}
	p, err := lookupProfile(ctx, c, ref)
	if err != nil {
		return "", err
	}
	return p.URN, nil
}

// lookupProfile reads the small top card of a profile: name, headline and URN.
func lookupProfile(ctx context.Context, c Client, ref ProfileRef) (Profile, error) {
	raw, err := dashProfile(ctx, c, ref, ProfileTopCardDecoration, ProfileShortDecoration)
	if err != nil {
		return Profile{}, err
	}
	details, err := ParseProfileView(raw, ref)
	if err != nil {
		return Profile{}, err
	}
	if memberIDFromURN(details.URN) == "" {
		return Profile{}, errs.New(errs.API, "could not find the member id of %s in LinkedIn's answer", ref)
	}
	return details.Profile, nil
}

// ProfilePosts returns up to limit recent posts by a member, newest first.
func ProfilePosts(ctx context.Context, c Client, ref ProfileRef, limit int) ([]FeedPost, error) {
	if limit < 1 || limit > MaxFeedPosts {
		return nil, errs.New(errs.Usage, "--limit must be between 1 and %d", MaxFeedPosts)
	}
	urn, err := MemberURN(ctx, c, ref)
	if err != nil {
		return nil, err
	}
	return paginatePosts(limit, func(start int) ([]byte, error) {
		return profileUpdatesQuery.Get(ctx, c, fmt.Sprintf("(count:%d,start:%d,profileUrn:%s)", FeedPageSize, start, restliValue(urn)))
	})
}
