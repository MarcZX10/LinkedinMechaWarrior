package api

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/voyager"
)

// Contact info is only served as a server-driven UI overlay: one POST that
// names the member by public id and name, answered with a React Server
// Components stream. The stream is a list of labelled fields, most wrapped in
// a tracking view named after the field (contact-email, contact-website...).
// Labels are in the session's language, so fields are read by view name
// where there is one.

const contactScreen = "com.linkedin.sdui.flagshipnav.profile.ProfileContactDetailsOverlay"

// ContactInfo is what a member shares in their contact info.
type ContactInfo struct {
	Email          string            `json:"email,omitempty"`
	Phones         []string          `json:"phones,omitempty"`
	Websites       []string          `json:"websites,omitempty"`
	Twitter        []string          `json:"twitter,omitempty"`
	Birthday       string            `json:"birthday,omitempty"`
	Address        string            `json:"address,omitempty"`
	ConnectedSince string            `json:"connected_since,omitempty"`
	Other          map[string]string `json:"other,omitempty"`
}

// GetContactInfo returns the contact info a member shares with you.
func GetContactInfo(ctx context.Context, c Client, ref ProfileRef) (ContactInfo, error) {
	profile, err := lookupProfile(ctx, c, ref)
	if err != nil {
		return ContactInfo{}, err
	}
	raw, err := c.PostSDUI(ctx, "navigation", voyager.Params{{Key: "screenId", Value: contactScreen}, {Key: "sduiid", Value: contactScreen}},
		map[string]any{
			"clientArguments": map[string]any{
				"$type":              "proto.sdui.actions.requests.RequestedArguments",
				"requestedStateKeys": []any{},
				"payload": map[string]any{
					"vanityName": profile.PublicID, "givenName": profile.FirstName, "familyName": profile.LastName,
					"isVanityNameResolved": true,
				},
				"requestMetadata":  map[string]any{"$type": "proto.sdui.common.RequestMetadata"},
				"states":           []any{},
				"screenId":         contactScreen,
				"knownTemplateIds": []any{},
			},
			"isModal": true,
		})
	if err != nil {
		return ContactInfo{}, err
	}
	return ParseContactInfo(raw)
}

var (
	rscLabel = regexp.MustCompile(`\["\$","p",null,\{"className":"[^"]*","children":\["((?:[^"\\]|\\.)+)"\]\}\]`)
	rscView  = regexp.MustCompile(`"viewName":"contact-([a-z-]+)"`)
	rscLink  = regexp.MustCompile(`"urlValue":\{"\$case":"url","url":"((?:[^"\\]|\\.)+)"`)
	rscText  = regexp.MustCompile(`"children":\["((?:[^"\\$]|\\.)(?:[^"\\]|\\.)*)"\]`)
	away     = regexp.MustCompile(`/safety/go/?\?url=([^&]+)`)
)

// ParseContactInfo reads the contact info overlay stream.
func ParseContactInfo(raw []byte) (ContactInfo, error) {
	stream := string(raw)
	start := strings.Index(stream, "profile-contact-info-details-view")
	if start < 0 {
		return ContactInfo{}, errs.New(errs.API, "LinkedIn's answer was not the contact info overlay; its format may have changed")
	}
	region := stream[start:]
	if end := strings.Index(region, `"viewName":"premium-upsell-card"`); end >= 0 {
		region = region[:end]
	}
	var info ContactInfo
	other := func(key, value string) {
		if value == "" {
			return
		}
		if info.Other == nil {
			info.Other = map[string]string{}
		}
		info.Other[key] = value
	}
	labels := rscLabel.FindAllStringSubmatchIndex(region, -1)
	before := 0
	for i, m := range labels {
		at, end := m[0], m[1]
		next := len(region)
		if i+1 < len(labels) {
			next = labels[i+1][0]
		}
		segment := region[end:next]
		view := ""
		if views := rscView.FindAllStringSubmatch(region[before:at], -1); len(views) > 0 {
			view = views[len(views)-1][1]
		}
		before = end
		label := strings.TrimSpace(unquote(region[m[2]:m[3]]))
		var links, texts []string
		for _, l := range rscLink.FindAllStringSubmatch(segment, -1) {
			links = append(links, linkTarget(unquote(l[1])))
		}
		for _, t := range rscText.FindAllStringSubmatch(segment, -1) {
			if text := strings.TrimSpace(unquote(t[1])); text != "" {
				texts = append(texts, text)
			}
		}
		text := strings.Join(texts, ", ")
		switch view {
		case "your-profile":
		case "email":
			info.Email = firstNonEmpty(firstOfList(links), text)
		case "website":
			info.Websites = append(info.Websites, links...)
		case "phone":
			info.Phones = append(info.Phones, texts...)
		case "twitter":
			info.Twitter = append(info.Twitter, texts...)
		case "birthday":
			info.Birthday = text
		case "address":
			info.Address = text
		case "":
			if strings.EqualFold(label, "connected since") {
				info.ConnectedSince = text
			} else {
				other(label, text)
			}
		default:
			other(view, text)
		}
	}
	return info, nil
}

func unquote(s string) string {
	var out string
	if err := json.Unmarshal([]byte(`"`+s+`"`), &out); err != nil {
		return s
	}
	return out
}

// linkTarget follows LinkedIn's safety redirect and drops mailto: and tel:.
func linkTarget(link string) string {
	if m := away.FindStringSubmatch(link); m != nil {
		if decoded, err := url.QueryUnescape(m[1]); err == nil {
			return decoded
		}
	}
	return strings.TrimPrefix(strings.TrimPrefix(link, "mailto:"), "tel:")
}

func firstOfList(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
