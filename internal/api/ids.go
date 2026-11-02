package api

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/errs"
)

// Commands accept what people copy from the browser: a profile, post,
// company or job address, or the bare identifier inside it.

// ProfileRef names a member by public identifier (the "ada-lovelace" in
// linkedin.com/in/ada-lovelace) or by member id (the "ACoAA..." in
// urn:li:fsd_profile:ACoAA...).
type ProfileRef struct {
	PublicID string
	MemberID string
}

var (
	profileURN = regexp.MustCompile(`^urn:li:(?:fsd_profile|fs_profile|fs_miniProfile|member):([A-Za-z0-9_-]+)$`)
	memberID   = regexp.MustCompile(`^ACo[A-Za-z0-9_-]{20,}$`)
	slug       = regexp.MustCompile(`^[^/\s?#]{2,100}$`)
)

// ParseProfile reads a profile address, URN, member id or public identifier.
func ParseProfile(input string) (ProfileRef, error) {
	input = strings.TrimSpace(input)
	if m := profileURN.FindStringSubmatch(input); m != nil {
		return ProfileRef{MemberID: m[1]}, nil
	}
	if memberID.MatchString(input) {
		return ProfileRef{MemberID: input}, nil
	}
	if segment, ok := pathAfter(input, "in"); ok {
		input = segment
	}
	if decoded, err := url.PathUnescape(input); err == nil {
		input = decoded
	}
	if !slug.MatchString(input) {
		return ProfileRef{}, errs.New(errs.Usage, "%q is not a LinkedIn profile; use its address (linkedin.com/in/NAME) or NAME", input)
	}
	return ProfileRef{PublicID: input}, nil
}

// String is the reference as it would be typed.
func (r ProfileRef) String() string {
	if r.PublicID != "" {
		return r.PublicID
	}
	return r.MemberID
}

var (
	postRef       = regexp.MustCompile(`urn:li:(activity|ugcPost|share):(\d+)`)
	encodedURN    = regexp.MustCompile(`urn%3Ali%3A(activity|ugcPost|share)%3A(\d+)`)
	postSlugID    = regexp.MustCompile(`-(activity|ugcPost|share)-(\d{6,})`)
	onlyDigits    = regexp.MustCompile(`^\d{6,}$`)
	jobIDInURL    = regexp.MustCompile(`(?:/jobs/view/(?:[^/?#]*-)?|currentJobId=|jobPosting:)(\d{6,})`)
	companyInPath = []string{"company", "school", "showcase"}
)

// ParsePost returns the URN of a post from its URN, its address, or its
// activity number.
func ParsePost(input string) (string, error) {
	input = strings.TrimSpace(input)
	for _, re := range []*regexp.Regexp{postRef, encodedURN, postSlugID} {
		if m := re.FindStringSubmatch(input); m != nil {
			return "urn:li:" + m[1] + ":" + m[2], nil
		}
	}
	if onlyDigits.MatchString(input) {
		return "urn:li:activity:" + input, nil
	}
	return "", errs.New(errs.Usage, "%q is not a LinkedIn post; use its address or URN (urn:li:activity:...)", input)
}

// ParseCompany returns a company's universal name (the "anthropic" in
// linkedin.com/company/anthropic) from its address or the name itself.
func ParseCompany(input string) (string, error) {
	input = strings.TrimSpace(input)
	for _, kind := range companyInPath {
		if segment, ok := pathAfter(input, kind); ok {
			input = segment
			break
		}
	}
	if !slug.MatchString(input) {
		return "", errs.New(errs.Usage, "%q is not a LinkedIn company; use its address (linkedin.com/company/NAME) or NAME", input)
	}
	return input, nil
}

// ParseJob returns the numeric id of a job posting from its address, URN or id.
func ParseJob(input string) (string, error) {
	input = strings.TrimSpace(input)
	if onlyDigits.MatchString(input) {
		return input, nil
	}
	if m := jobIDInURL.FindStringSubmatch(input); m != nil {
		return m[1], nil
	}
	return "", errs.New(errs.Usage, "%q is not a LinkedIn job; use its address (linkedin.com/jobs/view/ID) or ID", input)
}

// pathAfter returns the path segment that follows /kind/ in a LinkedIn
// address, e.g. "ada" for https://www.linkedin.com/in/ada/?trk=x.
func pathAfter(input, kind string) (string, bool) {
	if !strings.Contains(input, "linkedin.com/") && !strings.HasPrefix(input, "/"+kind+"/") {
		return "", false
	}
	if !strings.Contains(input, "://") && !strings.HasPrefix(input, "/") {
		input = "https://" + input
	}
	u, err := url.Parse(input)
	if err != nil {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == kind && parts[i+1] != "" {
			return parts[i+1], true
		}
	}
	return "", false
}
