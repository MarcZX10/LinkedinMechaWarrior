package api

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/normalized"
)

// ProfileViews is LinkedIn's overview of who viewed your profile lately.
type ProfileViews struct {
	// Views is LinkedIn's count of profile views, when it gives one.
	Views *int `json:"views"`
	// Change is how views changed against the period before, in percent.
	Change *float64 `json:"change_percent,omitempty"`
	// Viewers are the viewers LinkedIn names, latest first. Free accounts
	// see only a few of them.
	Viewers []Viewer `json:"viewers"`
	// Private counts viewers browsing in private mode, or that LinkedIn
	// only describes ("someone at a software company").
	Private int `json:"private"`
}

// Viewer is one person who viewed your profile.
type Viewer struct {
	Name     string `json:"name"`
	Headline string `json:"headline,omitempty"`
	URL      string `json:"url,omitempty"`
	Degree   string `json:"degree,omitempty"`
	Viewed   string `json:"viewed,omitempty"`
	viewedAt int64
}

var degrees = map[string]string{"DISTANCE_1": "1st", "DISTANCE_2": "2nd", "DISTANCE_3": "3rd", "OUT_OF_NETWORK": "3rd+"}

// GetProfileViews reads the overview of the "Who viewed your profile" page
// (one request).
func GetProfileViews(ctx context.Context, c Client) (ProfileViews, error) {
	raw, err := c.Get(ctx, "/identity/wvmpCards", nil)
	if err != nil {
		return ProfileViews{}, err
	}
	return ParseProfileViews(raw), nil
}

// ParseProfileViews reads the overview. Its cards are Rest.li unions, which
// come either keyed by type or with a $type, so it looks for them anywhere.
func ParseProfileViews(raw []byte) ProfileViews {
	doc := normalized.Parse(raw)
	views := ProfileViews{Viewers: []Viewer{}}
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case []any:
			for _, item := range v {
				walk(item)
			}
		case normalized.Object:
			kind, card := union(v)
			switch {
			case strings.HasSuffix(kind, "WvmpProfileViewCard"):
				viewerKind, viewer := union(card["viewer"])
				if !strings.HasSuffix(viewerKind, "FullProfileViewer") {
					views.Private++
					return
				}
				profile, _ := viewer["profile"].(normalized.Object)
				mini := doc.Ref(profile, "miniProfile")
				name := strings.TrimSpace(normalized.String(mini["firstName"]) + " " + normalized.String(mini["lastName"]))
				at, _ := normalized.Int(card["viewedAt"])
				key := firstNonEmpty(normalized.String(mini["entityUrn"]), name) + "@" + strconv.Itoa(at)
				if name == "" || seen[key] {
					return
				}
				seen[key] = true
				url := ""
				if id := normalized.String(mini["publicIdentifier"]); id != "" {
					url = "https://www.linkedin.com/in/" + id + "/"
				}
				views.Viewers = append(views.Viewers, Viewer{
					Name: name, Headline: normalized.String(mini["occupation"]), URL: url,
					Degree: degrees[normalized.String(normalized.Dig(profile, "distance", "value"))],
					Viewed: millisDateTime(card["viewedAt"]), viewedAt: int64(at),
				})
				return
			case strings.HasSuffix(kind, "PrivateProfileViewer"):
				views.Private++
				return
			case strings.HasSuffix(kind, "WvmpSummaryInsightCard"):
				if n, ok := normalized.Int(card["numViews"]); ok && (views.Views == nil || n > *views.Views) {
					views.Views = &n
				}
				if change, ok := card["numViewsChangeInPercentage"].(float64); ok {
					views.Change = &change
				}
			}
			for _, value := range card {
				walk(value)
			}
		}
	}
	walk(doc.Data)
	for _, entity := range doc.Included {
		walk(entity)
	}
	sort.SliceStable(views.Viewers, func(i, j int) bool { return views.Viewers[i].viewedAt > views.Viewers[j].viewedAt })
	return views
}

// union returns the type and the object of a Rest.li union value, written
// either {"com.linkedin...Type": {...}} or {..., "$type": "com.linkedin...Type"}.
// Plain objects come back with their own $type, if any.
func union(v any) (string, normalized.Object) {
	obj, _ := v.(normalized.Object)
	if obj == nil {
		return "", nil
	}
	if inner, ok := obj["value"].(normalized.Object); ok && len(obj) <= 3 {
		if kind, value := union(inner); strings.HasPrefix(kind, "com.linkedin.") {
			return kind, value
		}
	}
	if kind, ok := obj["$type"].(string); ok {
		return kind, obj
	}
	if len(obj) == 1 {
		for key, value := range obj {
			if inner, ok := value.(normalized.Object); ok && strings.HasPrefix(key, "com.linkedin.") {
				return key, inner
			}
		}
	}
	return "", obj
}
