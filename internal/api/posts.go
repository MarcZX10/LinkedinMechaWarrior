package api

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// PublishedPost identifies a post created by Publish.
type PublishedPost struct {
	URN string `json:"urn,omitempty"`
	URL string `json:"url,omitempty"`
	// Scheduled is when LinkedIn will publish a scheduled post.
	Scheduled string `json:"scheduled_for,omitempty"`
}

// Visibility controls who can see a new post.
type Visibility int

const (
	// Anyone makes the post public.
	Anyone Visibility = iota
	// ConnectionsOnly limits the post to your connections.
	ConnectionsOnly
)

// Publish creates a post, after uploading its attachments or creating its
// poll. The caller is responsible for getting the user's confirmation first.
func Publish(ctx context.Context, c Client, text string, visibility Visibility, opts PostOptions) (PublishedPost, error) {
	media, err := prepareMedia(ctx, c, text, opts)
	if err != nil {
		return PublishedPost{}, err
	}
	raw, err := c.Post(ctx, "/contentcreation/normShares", nil, SharePayload(text, visibility, media, opts.At))
	if err != nil {
		return PublishedPost{}, err
	}
	post := ParsePublished(raw)
	if !opts.At.IsZero() {
		post.Scheduled = opts.At.Format(time.RFC3339)
	}
	return post, nil
}

var postURN = regexp.MustCompile(`urn:li:(?:activity|share|ugcPost):\d+`)

// ParsePublished finds the new post's URN in the response, wherever it is.
func ParsePublished(raw []byte) PublishedPost {
	var post PublishedPost
	for _, match := range postURN.FindAllString(string(raw), -1) {
		// The activity URN is the one that works in /feed/update/ links.
		if post.URN == "" || strings.HasPrefix(match, "urn:li:activity:") {
			post.URN = match
		}
	}
	if post.URN != "" {
		post.URL = "https://www.linkedin.com/feed/update/" + post.URN + "/"
	}
	return post
}
