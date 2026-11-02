package api

import (
	"context"
	"testing"
)

// The overview as a normalized response: cards in included, the viewer's
// mini profile referenced.
const viewsNormalized = `{"data":{"*elements":["urn:li:fs_wvmpCard:1"]},"included":[
 {"entityUrn":"urn:li:fs_wvmpCard:1","$type":"com.linkedin.voyager.identity.me.Card","value":{
  "$type":"com.linkedin.voyager.identity.me.wvmpOverview.WvmpViewersCard",
  "insightCards":[{"value":{"$type":"com.linkedin.voyager.identity.me.wvmpOverview.WvmpSummaryInsightCard","numViews":42,"numViewsChangeInPercentage":12.5,
   "cards":[
    {"value":{"$type":"com.linkedin.voyager.identity.me.WvmpProfileViewCard","viewedAt":1700000000000,
     "viewer":{"$type":"com.linkedin.voyager.identity.me.FullProfileViewer","profile":{"*miniProfile":"urn:li:fs_miniProfile:ACoAAA1","distance":{"value":"DISTANCE_2"}}}}},
    {"value":{"$type":"com.linkedin.voyager.identity.me.WvmpProfileViewCard","viewedAt":1700100000000,
     "viewer":{"$type":"com.linkedin.voyager.identity.me.FullProfileViewer","profile":{"*miniProfile":"urn:li:fs_miniProfile:ACoAAA2","distance":{"value":"DISTANCE_1"}}}}},
    {"value":{"$type":"com.linkedin.voyager.identity.me.WvmpProfileViewCard","viewedAt":1700000000000,
     "viewer":{"$type":"com.linkedin.voyager.identity.me.ObfuscatedProfileViewer","headline":"Someone at a software company"}}},
    {"value":{"$type":"com.linkedin.voyager.identity.me.WvmpGenericCard","text":"1 person with the job title Recruiter"}}
   ]}}]}},
 {"entityUrn":"urn:li:fs_miniProfile:ACoAAA1","firstName":"Grace","lastName":"Hopper","occupation":"Admiral","publicIdentifier":"grace-hopper"},
 {"entityUrn":"urn:li:fs_miniProfile:ACoAAA2","firstName":"Alan","lastName":"Turing","occupation":"Mathematician","publicIdentifier":"alan-turing"}
]}`

// The same overview with unions keyed by type and profiles inline.
const viewsUnions = `{"data":{"elements":[{"value":{"com.linkedin.voyager.identity.me.wvmpOverview.WvmpViewersCard":{
 "insightCards":[{"value":{"com.linkedin.voyager.identity.me.wvmpOverview.WvmpSummaryInsightCard":{"numViewsChangeInPercentage":613.0,"cards":[
  {"value":{"com.linkedin.voyager.identity.me.WvmpProfileViewCard":{"viewedAt":1700000000000,"viewer":{"com.linkedin.voyager.identity.me.FullProfileViewer":{
   "profile":{"miniProfile":{"firstName":"Jane","lastName":"Doe","occupation":"Software Engineer at Example Corp","entityUrn":"urn:li:fs_miniProfile:ACoAABx","publicIdentifier":"jane-doe-123"}}}}}}},
  {"value":{"com.linkedin.voyager.identity.me.PrivateProfileViewer":{"headline":"Someone in private mode"}}}
 ]}}}]}}}]},"included":[]}`

func TestProfileViews(t *testing.T) {
	r := &routes{replies: map[string]string{"/identity/wvmpCards": viewsNormalized}}
	v, err := GetProfileViews(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if v.Views == nil || *v.Views != 42 || v.Change == nil || *v.Change != 12.5 || v.Private != 1 || len(v.Viewers) != 2 {
		t.Fatalf("views %+v", v)
	}
	if first := v.Viewers[0]; first.Name != "Alan Turing" || first.Degree != "1st" || first.URL != "https://www.linkedin.com/in/alan-turing/" {
		t.Errorf("latest viewer first, got %+v", first)
	}
	if second := v.Viewers[1]; second.Name != "Grace Hopper" || second.Headline != "Admiral" || second.Degree != "2nd" || second.Viewed == "" {
		t.Errorf("second viewer %+v", second)
	}

	v = ParseProfileViews([]byte(viewsUnions))
	if v.Views != nil || v.Change == nil || *v.Change != 613 || v.Private != 1 || len(v.Viewers) != 1 || v.Viewers[0].Name != "Jane Doe" ||
		v.Viewers[0].URL != "https://www.linkedin.com/in/jane-doe-123/" {
		t.Errorf("unions %+v", v)
	}
	if v := ParseProfileViews([]byte(`{"data":{},"included":[]}`)); v.Viewers == nil || len(v.Viewers) != 0 {
		t.Errorf("empty overview %+v", v)
	}
}
