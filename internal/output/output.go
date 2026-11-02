// Package output formats results for the terminal.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/api"
)

const (
	maxWidth     = 100
	previewLines = 6
)

// JSON writes v as indented JSON.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// Width is the terminal width, capped for readability.
func Width() int {
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 20 && width < maxWidth {
		return width
	}
	return maxWidth
}

// Wrap breaks text into lines of at most width characters, keeping paragraphs.
func Wrap(text string, width int, indent string) []string {
	var lines []string
	avail := width - utf8.RuneCountInString(indent)
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) > avail {
				lines = append(lines, indent+line)
				line = word
				continue
			}
			line += " " + word
		}
		lines = append(lines, indent+line)
	}
	return lines
}

// Profile formats a profile.
func Profile(p api.Profile) string {
	lines := []string{p.Name}
	if p.Headline != "" {
		lines = append(lines, Wrap(p.Headline, Width(), "")...)
	}
	if p.URL != "" {
		lines = append(lines, p.URL)
	}
	return strings.Join(lines, "\n")
}

// FeedPost formats a feed post; full disables truncation of long texts.
func FeedPost(p api.FeedPost, full bool) string {
	width := Width()
	title := firstNonEmpty(p.Author, "(unknown author)")
	if p.Age != "" {
		title += " · " + p.Age
	}
	if p.Promoted {
		title += " · promoted"
	}
	lines := []string{title}
	if p.AuthorHeadline != "" {
		lines = append(lines, truncate(p.AuthorHeadline, width))
	}
	if p.Context != "" {
		lines = append(lines, "("+p.Context+")")
	}
	if p.ResharedAuthor != "" {
		lines = append(lines, "Reposting "+p.ResharedAuthor)
	}
	if p.Text != "" {
		body := Wrap(p.Text, width, "  ")
		if !full && len(body) > previewLines {
			body = append(body[:previewLines], "  … (use --full to see everything)")
		}
		lines = append(lines, "")
		lines = append(lines, body...)
	}

	var stats []string
	for _, s := range []struct {
		n     *int
		label string
	}{{p.Reactions, "reactions"}, {p.Comments, "comments"}, {p.Reposts, "reposts"}} {
		if s.n != nil {
			stats = append(stats, fmt.Sprintf("%d %s", *s.n, s.label))
		}
	}
	footer := strings.Join(stats, " · ")
	if p.URL != "" {
		footer = strings.TrimSpace(footer + "   " + p.URL)
	}
	if footer != "" {
		lines = append(lines, "", footer)
	}
	return strings.Join(lines, "\n")
}

// PostPreview frames a post with its length.
func PostPreview(text string) string {
	separator := strings.Repeat("-", min(Width(), 60))
	return fmt.Sprintf("%s\n%s\n%s\n%d characters", separator, text, separator, utf8.RuneCountInString(text))
}

func truncate(text string, width int) string {
	if utf8.RuneCountInString(text) <= width {
		return text
	}
	runes := []rune(text)
	return string(runes[:width-1]) + "…"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ProfileDetails formats a full profile.
func ProfileDetails(p api.ProfileDetails) string {
	width := Width()
	lines := []string{p.Name}
	if p.Headline != "" {
		lines = append(lines, Wrap(p.Headline, width, "")...)
	}
	if facts := joinNonEmpty(" · ", p.Location, p.Industry); facts != "" {
		lines = append(lines, facts)
	}
	if p.URL != "" {
		lines = append(lines, p.URL)
	}
	if p.About != "" {
		lines = append(lines, "", "About")
		lines = append(lines, Wrap(p.About, width, "  ")...)
	}
	if len(p.Experience) > 0 {
		lines = append(lines, "", "Experience")
		for _, pos := range p.Experience {
			lines = append(lines, "  "+joinNonEmpty(" · ", pos.Title, pos.Company))
			if details := joinNonEmpty(" · ", period(pos.Start, pos.End), pos.Location); details != "" {
				lines = append(lines, "    "+details)
			}
		}
	}
	if len(p.Education) > 0 {
		lines = append(lines, "", "Education")
		for _, edu := range p.Education {
			lines = append(lines, "  "+edu.School)
			if details := joinNonEmpty(" · ", joinNonEmpty(", ", edu.Degree, edu.Field), period(edu.Start, edu.End)); details != "" {
				lines = append(lines, "    "+details)
			}
		}
	}
	if len(p.Skills) > 0 {
		lines = append(lines, "", "Skills")
		lines = append(lines, Wrap(strings.Join(p.Skills, ", "), width, "  ")...)
	}
	return strings.Join(lines, "\n")
}

// ContactInfo formats a member's contact info.
func ContactInfo(c api.ContactInfo) string {
	var lines []string
	add := func(label string, values ...string) {
		for _, v := range values {
			if v != "" {
				lines = append(lines, fmt.Sprintf("%-9s %s", label+":", v))
			}
		}
	}
	add("Email", c.Email)
	add("Phone", c.Phones...)
	add("Website", c.Websites...)
	add("Twitter", c.Twitter...)
	add("Birthday", c.Birthday)
	add("Address", c.Address)
	if len(lines) == 0 {
		return "No contact info shared with you."
	}
	return strings.Join(lines, "\n")
}

func period(start, end string) string {
	switch {
	case start == "" && end == "":
		return ""
	case end == "":
		return start + " – present"
	}
	return start + " – " + end
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

// Invitations formats a list of invitations with the ids used to answer them.
func Invitations(invitations []api.Invitation) string {
	width := Width()
	blocks := make([]string, len(invitations))
	for i, inv := range invitations {
		lines := []string{joinNonEmpty(" · ", inv.Name, inv.Sent) + "   id " + inv.ID}
		if inv.Headline != "" {
			lines = append(lines, "  "+truncate(inv.Headline, width-2))
		}
		if inv.Message != "" {
			lines = append(lines, Wrap("“"+inv.Message+"”", width, "  ")...)
		}
		if inv.Profile != "" {
			lines = append(lines, "  "+inv.Profile)
		}
		blocks[i] = strings.Join(lines, "\n")
	}
	return strings.Join(blocks, "\n\n")
}

// Connections formats a list of connections, one per line.
func Connections(connections []api.Connection) string {
	width := Width()
	lines := make([]string, len(connections))
	for i, c := range connections {
		line := joinNonEmpty(" · ", c.Name, c.Headline)
		if c.URL != "" {
			line = truncate(line, max(20, width-len(c.URL)-3)) + "   " + c.URL
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// Notifications formats notifications, unread ones marked with a dot.
func Notifications(notifications []api.Notification) string {
	width := Width()
	blocks := make([]string, len(notifications))
	for i, n := range notifications {
		marker := "  "
		if !n.Read {
			marker = "• "
		}
		lines := Wrap(n.Text, width, "  ")
		lines[0] = marker + strings.TrimPrefix(lines[0], "  ")
		if details := joinNonEmpty("   ", n.Date, n.URL); details != "" {
			lines = append(lines, "  "+details)
		}
		blocks[i] = strings.Join(lines, "\n")
	}
	return strings.Join(blocks, "\n\n")
}

// Comments formats comments with their authors.
func Comments(comments []api.Comment) string {
	width := Width()
	blocks := make([]string, len(comments))
	for i, c := range comments {
		lines := []string{joinNonEmpty(" · ", firstNonEmpty(c.Author, "(unknown author)"), c.Date)}
		if c.AuthorHeadline != "" {
			lines = append(lines, truncate(c.AuthorHeadline, width))
		}
		lines = append(lines, Wrap(c.Text, width, "  ")...)
		var stats []string
		if c.Reactions != nil && *c.Reactions > 0 {
			stats = append(stats, fmt.Sprintf("%d reactions", *c.Reactions))
		}
		if c.Replies != nil && *c.Replies > 0 {
			stats = append(stats, fmt.Sprintf("%d replies", *c.Replies))
		}
		if c.URN != "" {
			stats = append(stats, c.URN)
		}
		if len(stats) > 0 {
			lines = append(lines, "  "+strings.Join(stats, " · "))
		}
		blocks[i] = strings.Join(lines, "\n")
	}
	return strings.Join(blocks, "\n\n")
}

// Reactions formats who reacted, one per line.
func Reactions(reactions []api.Reaction) string {
	width := Width()
	lines := make([]string, len(reactions))
	for i, r := range reactions {
		lines[i] = truncate(fmt.Sprintf("%-10s %s", r.Type, joinNonEmpty(" · ", r.Name, r.Headline)), width)
	}
	return strings.Join(lines, "\n")
}

// Conversations formats an inbox, unread conversations marked with a dot.
func Conversations(conversations []api.Conversation) string {
	width := Width()
	blocks := make([]string, len(conversations))
	for i, c := range conversations {
		marker := "  "
		if c.Unread > 0 {
			marker = "• "
		}
		with := firstNonEmpty(c.Title, strings.Join(c.With, ", "), "(no participants)")
		lines := []string{marker + joinNonEmpty(" · ", with, c.Date) + "   id " + c.ID}
		if c.LastMessage != "" {
			last := c.LastMessage
			if c.LastSender != "" {
				last = c.LastSender + ": " + last
			}
			lines = append(lines, "  "+truncate(strings.Join(strings.Fields(last), " "), width-2))
		}
		blocks[i] = strings.Join(lines, "\n")
	}
	return strings.Join(blocks, "\n\n")
}

// Messages formats a conversation like a chat, oldest first.
func Messages(messages []api.Message) string {
	width := Width()
	blocks := make([]string, len(messages))
	for i, m := range messages {
		lines := []string{joinNonEmpty(" · ", firstNonEmpty(m.Sender, "(unknown)"), m.Date)}
		lines = append(lines, Wrap(m.Text, width, "  ")...)
		blocks[i] = strings.Join(lines, "\n")
	}
	return strings.Join(blocks, "\n\n")
}

// SearchResults formats search results.
func SearchResults(results []api.SearchResult) string {
	width := Width()
	blocks := make([]string, len(results))
	for i, r := range results {
		lines := []string{r.Name}
		if details := joinNonEmpty(" · ", r.Subtitle, r.Location); details != "" {
			lines = append(lines, "  "+truncate(details, width-2))
		}
		if r.URL != "" {
			lines = append(lines, "  "+r.URL)
		}
		blocks[i] = strings.Join(lines, "\n")
	}
	return strings.Join(blocks, "\n\n")
}

// Company formats a company page.
func Company(c api.Company) string {
	width := Width()
	lines := []string{c.Name}
	if c.Tagline != "" {
		lines = append(lines, Wrap(c.Tagline, width, "")...)
	}
	facts := []string{strings.Join(c.Industries, ", "), c.Headquarters, c.Size}
	if c.Followers != nil {
		facts = append(facts, fmt.Sprintf("%d followers", *c.Followers))
	}
	if c.Founded != nil {
		facts = append(facts, fmt.Sprintf("founded %d", *c.Founded))
	}
	if line := joinNonEmpty(" · ", facts...); line != "" {
		lines = append(lines, Wrap(line, width, "")...)
	}
	for _, link := range []string{c.Website, c.URL} {
		if link != "" {
			lines = append(lines, link)
		}
	}
	if c.Description != "" {
		lines = append(lines, "")
		lines = append(lines, Wrap(c.Description, width, "  ")...)
	}
	return strings.Join(lines, "\n")
}

// Jobs formats job search results.
func Jobs(jobs []api.Job) string {
	width := Width()
	blocks := make([]string, len(jobs))
	for i, j := range jobs {
		lines := []string{j.Title + "   id " + j.ID}
		if details := joinNonEmpty(" · ", j.Company, j.Location); details != "" {
			lines = append(lines, "  "+truncate(details, width-2))
		}
		lines = append(lines, "  "+j.URL)
		blocks[i] = strings.Join(lines, "\n")
	}
	return strings.Join(blocks, "\n\n")
}

// Job formats a job posting; full disables truncation of the description.
func Job(j api.Job, full bool) string {
	width := Width()
	lines := []string{j.Title}
	if line := joinNonEmpty(" · ", j.Company, j.Location, j.Workplace); line != "" {
		lines = append(lines, line)
	}
	facts := []string{}
	if j.Listed != "" {
		facts = append(facts, "listed "+j.Listed)
	}
	if j.Applicants != nil {
		facts = append(facts, fmt.Sprintf("%d applicants", *j.Applicants))
	}
	if line := joinNonEmpty(" · ", facts...); line != "" {
		lines = append(lines, line)
	}
	lines = append(lines, j.URL)
	if j.ApplyURL != "" {
		lines = append(lines, "Apply: "+j.ApplyURL)
	}
	if j.Description != "" {
		body := Wrap(j.Description, width, "  ")
		if !full && len(body) > 3*previewLines {
			body = append(body[:3*previewLines], "  … (use --full to see everything)")
		}
		lines = append(lines, "")
		lines = append(lines, body...)
	}
	return strings.Join(lines, "\n")
}

// SavedPosts formats saved posts with their links.
func SavedPosts(posts []api.SavedPost) string {
	width := Width()
	blocks := make([]string, len(posts))
	for i, p := range posts {
		lines := []string{joinNonEmpty(" · ", firstNonEmpty(p.Author, "(unknown author)"), p.Age)}
		if p.AuthorHeadline != "" {
			lines = append(lines, truncate(p.AuthorHeadline, width))
		}
		if p.Text != "" {
			body := Wrap(p.Text, width, "  ")
			if len(body) > 3 {
				body = append(body[:3], "  …")
			}
			lines = append(lines, body...)
		}
		lines = append(lines, "  "+p.URL)
		blocks[i] = strings.Join(lines, "\n")
	}
	return strings.Join(blocks, "\n\n")
}
