// Package auth finds LinkedIn session cookies in the browsers installed on
// this computer, so you log in with your everyday browser and lmw reuses
// that session instead of logging in on its own.
package auth

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all" // register every supported browser

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
)

// Candidate is a LinkedIn session found in one browser profile.
type Candidate struct {
	Browser string
	Profile string
	Cookies map[string]string
	// Created is when the session cookie was set, i.e. when you logged in.
	Created time.Time
}

// Label describes where the session was found.
func (c Candidate) Label() string {
	if c.Profile == "" {
		return c.Browser
	}
	return c.Browser + " (" + c.Profile + ")"
}

// FindSessions looks for LinkedIn sessions in installed browsers, newest
// first. browser limits the search to one browser by name ("" for all).
func FindSessions(ctx context.Context, browser string) ([]Candidate, error) {
	var candidates []Candidate
	now := time.Now()
	for _, store := range kooky.FindAllCookieStores(ctx) {
		if browser != "" && !strings.EqualFold(store.Browser(), browser) {
			continue
		}
		candidate := Candidate{Browser: store.Browser(), Profile: store.Profile(), Cookies: map[string]string{}}
		domains := map[string]string{}
		// Not kooky.Valid: it applies net/http's value rules, which reject the
		// quotes LinkedIn always puts in JSESSIONID ("ajax:...").
		for cookie := range store.TraverseCookies(kooky.DomainHasSuffix("linkedin.com")).OnlyCookies() {
			if !cookie.Expires.IsZero() && cookie.Expires.Before(now) {
				continue
			}
			if !printable(cookie.Value) {
				// Undecryptable values come back as garbage (e.g. Chrome's
				// app-bound encryption on Windows); never send those.
				continue
			}
			// When a name exists for several domains, prefer www.linkedin.com.
			if previous, ok := domains[cookie.Name]; ok && strings.Contains(previous, "www.") {
				continue
			}
			domains[cookie.Name] = cookie.Domain
			candidate.Cookies[cookie.Name] = cookie.Value
			if cookie.Name == "li_at" {
				candidate.Created = cookie.Creation
			}
		}
		_ = store.Close()
		if candidate.Cookies["li_at"] != "" && candidate.Cookies["JSESSIONID"] != "" {
			candidates = append(candidates, candidate)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Created.After(candidates[j].Created) })
	if len(candidates) == 0 {
		where := "any installed browser"
		if browser != "" {
			where = browser
		}
		return nil, errs.New(errs.AuthRequired,
			"no LinkedIn session found in %s. Log in to LinkedIn in your browser first. "+
				"If the browser is open, close it and retry (some lock their cookie database), "+
				"or import from a HAR file or by pasting the cookies (`lmw auth import --help`)", where)
	}
	return candidates, nil
}

func printable(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}
