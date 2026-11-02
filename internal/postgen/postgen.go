// Package postgen builds post outlines offline and validates post text.
//
// The generator does not invent content about a topic it knows nothing about.
// It produces a structure (hook, sections to fill in, closing question) with
// [placeholders] to replace before posting; Validate refuses text that still
// has them.
package postgen

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
)

var (
	// Tones lists the supported tones.
	Tones = []string{"casual", "educational", "founder", "professional", "technical"}
	// Lengths lists the supported lengths.
	Lengths = []string{"long", "medium", "short"}

	placeholder = regexp.MustCompile(`\[[^\]\n]{3,}\]`)

	hooks = map[string]string{
		"professional": "Something I've been thinking about lately: ",
		"technical":    "A technical lesson worth sharing: ",
		"casual":       "Quick one today: ",
		"founder":      "Building a company teaches you things nothing else does. This week: ",
		"educational":  "Here's a simple way to think about it: ",
	}
	sections = map[string][]string{
		"technical": {
			"[The context: what you were building or fixing, in one or two sentences.]",
			"[The technical detail: the approach, the trade-off, or the result.]",
			"[A concrete number, snippet, or before/after that makes it credible.]",
			"[What you'd do differently, or what you're trying next.]",
		},
		"educational": {
			"[The core idea, explained in plain words.]",
			"[A simple example that makes it click.]",
			"[A common mistake people make with it.]",
			"[One practical step readers can take today.]",
		},
	}
	defaultSections = []string{
		"[What happened or what you noticed, in one or two sentences.]",
		"[Why it matters: the insight or the lesson.]",
		"[A concrete example, number, or detail that makes it credible.]",
		"[What you'd do differently, or what you're trying next.]",
	}
	sectionCount = map[string]int{"short": 1, "medium": 2, "long": 4}
	closings     = map[string]string{
		"professional": "What has your experience been?",
		"technical":    "How would you have approached it?",
		"casual":       "Has anything similar happened to you?",
		"founder":      "Founders, what would you add?",
		"educational":  "What would you add to this?",
	}
)

// Draft is a generated post outline.
type Draft struct {
	Text   string `json:"text"`
	Tone   string `json:"tone"`
	Length string `json:"length"`
}

// Generate builds an outline for an idea.
func Generate(idea, tone, length string) (Draft, error) {
	idea = strings.Join(strings.Fields(idea), " ")
	if utf8.RuneCountInString(idea) < 8 {
		return Draft{}, errs.New(errs.Usage, "the idea is too short to build a useful post")
	}
	tone, length = strings.ToLower(strings.TrimSpace(tone)), strings.ToLower(strings.TrimSpace(length))
	if _, ok := hooks[tone]; !ok {
		return Draft{}, errs.New(errs.Usage, "unsupported tone %q; use one of: %s", tone, strings.Join(Tones, ", "))
	}
	count, ok := sectionCount[length]
	if !ok {
		return Draft{}, errs.New(errs.Usage, "unsupported length %q; use one of: %s", length, strings.Join(Lengths, ", "))
	}

	body := sections[tone]
	if body == nil {
		body = defaultSections
	}
	parts := append([]string{hooks[tone] + asSentence(idea)}, body[:count]...)
	parts = append(parts, closings[tone])
	return Draft{Text: strings.Join(parts, "\n\n"), Tone: tone, Length: length}, nil
}

// Placeholders returns the unfilled [placeholders] in a text.
func Placeholders(text string) []string {
	return placeholder.FindAllString(text, -1)
}

// Validate checks that a post can be published.
func Validate(text string, maxChars int) error {
	if strings.TrimSpace(text) == "" {
		return errs.New(errs.Validation, "the post is empty")
	}
	if n := utf8.RuneCountInString(text); n > maxChars {
		return errs.New(errs.Validation, "the post is %d characters long; the limit is %d", n, maxChars)
	}
	if found := Placeholders(text); len(found) > 0 {
		return errs.New(errs.Validation, "the post still has template placeholders to fill in, e.g. %s", found[0])
	}
	return nil
}

// Normalize trims trailing spaces on each line and surrounding blank lines.
func Normalize(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// asSentence ends the idea with exactly one terminator, keeping questions and exclamations.
func asSentence(idea string) string {
	if strings.HasSuffix(idea, "?") || strings.HasSuffix(idea, "!") {
		return idea
	}
	return strings.TrimRight(idea, ".!,;: ") + "."
}

func init() {
	sort.Strings(Tones)
	sort.Strings(Lengths)
}
