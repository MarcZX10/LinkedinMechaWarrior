package api

import "testing"

func TestParseProfile(t *testing.T) {
	for input, want := range map[string]ProfileRef{
		"ada-lovelace": {PublicID: "ada-lovelace"},
		"https://www.linkedin.com/in/ada-lovelace/":        {PublicID: "ada-lovelace"},
		"linkedin.com/in/ada-lovelace?trk=public":          {PublicID: "ada-lovelace"},
		"https://es.linkedin.com/in/jos%C3%A9-garc%C3%ADa": {PublicID: "josé-garcía"},
		"urn:li:fsd_profile:ACoAABcdEfGh123456789012345":   {MemberID: "ACoAABcdEfGh123456789012345"},
		"ACoAABcdEfGh123456789012345":                      {MemberID: "ACoAABcdEfGh123456789012345"},
	} {
		got, err := ParseProfile(input)
		if err != nil || got != want {
			t.Errorf("ParseProfile(%q) = %+v, %v; want %+v", input, got, err, want)
		}
	}
	for _, bad := range []string{"", "a", "two words", "https://www.linkedin.com/feed/"} {
		if _, err := ParseProfile(bad); err == nil {
			t.Errorf("ParseProfile(%q) should fail", bad)
		}
	}
}

func TestParsePost(t *testing.T) {
	for input, want := range map[string]string{
		"urn:li:activity:7123456789012345678":                                                  "urn:li:activity:7123456789012345678",
		"https://www.linkedin.com/feed/update/urn:li:activity:7123456789012345678/":            "urn:li:activity:7123456789012345678",
		"https://www.linkedin.com/feed/update/urn%3Ali%3AugcPost%3A7123456789012345678":        "urn:li:ugcPost:7123456789012345678",
		"https://www.linkedin.com/posts/ada-lovelace_engines-activity-7123456789012345678-AbC": "urn:li:activity:7123456789012345678",
		"7123456789012345678": "urn:li:activity:7123456789012345678",
	} {
		if got, err := ParsePost(input); err != nil || got != want {
			t.Errorf("ParsePost(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := ParsePost("https://www.linkedin.com/in/ada/"); err == nil {
		t.Error("a profile address is not a post")
	}
}

func TestParseCompanyAndJob(t *testing.T) {
	for input, want := range map[string]string{
		"anthropic": "anthropic",
		"https://www.linkedin.com/company/anthropic/about/": "anthropic",
		"https://www.linkedin.com/school/urv/":              "urv",
	} {
		if got, err := ParseCompany(input); err != nil || got != want {
			t.Errorf("ParseCompany(%q) = %q, %v", input, got, err)
		}
	}
	for input, want := range map[string]string{
		"4012345678": "4012345678",
		"https://www.linkedin.com/jobs/view/4012345678/":                        "4012345678",
		"https://www.linkedin.com/jobs/view/go-engineer-at-acme-4012345678":     "4012345678",
		"https://www.linkedin.com/jobs/search/?currentJobId=4012345678&geoId=1": "4012345678",
		"urn:li:fsd_jobPosting:4012345678":                                      "4012345678",
	} {
		if got, err := ParseJob(input); err != nil || got != want {
			t.Errorf("ParseJob(%q) = %q, %v", input, got, err)
		}
	}
	if _, err := ParseJob("engineer"); err == nil {
		t.Error("expected an error for a job without id")
	}
}
