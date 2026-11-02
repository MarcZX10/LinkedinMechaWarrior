package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/voyager"
)

// call is one request made through routes.
type call struct {
	method  string
	path    string
	params  voyager.Params
	payload any
}

func (c call) param(key string) string {
	for _, p := range c.params {
		if p.Key == key {
			return p.Value
		}
	}
	return ""
}

// body returns the JSON payload of a POST as a string.
func (c call) body() string {
	raw, _ := json.Marshal(c.payload)
	return string(raw)
}

// routes is a fake Client that answers by "METHOD /path" or "/path". A path
// may include its query string for endpoints that keep parameters in it.
type routes struct {
	replies map[string]string
	calls   []call
}

func (r *routes) reply(method, path string, params voyager.Params, payload any) ([]byte, error) {
	r.calls = append(r.calls, call{method, path, params, payload})
	base, _, _ := strings.Cut(path, "?")
	for _, key := range []string{method + " " + path, path, method + " " + base, base} {
		if body, ok := r.replies[key]; ok {
			return []byte(body), nil
		}
	}
	return nil, fmt.Errorf("unexpected %s %s", method, path)
}

func (r *routes) Get(_ context.Context, path string, params voyager.Params) ([]byte, error) {
	return r.reply("GET", path, params, nil)
}

func (r *routes) Post(_ context.Context, path string, params voyager.Params, payload any) ([]byte, error) {
	return r.reply("POST", path, params, payload)
}

func (r *routes) Delete(_ context.Context, path string, params voyager.Params) ([]byte, error) {
	return r.reply("DELETE", path, params, nil)
}

func (r *routes) PostSDUI(_ context.Context, action string, params voyager.Params, payload any) ([]byte, error) {
	return r.reply("SDUI", action, params, payload)
}

func (r *routes) last(t *testing.T) call {
	t.Helper()
	if len(r.calls) == 0 {
		t.Fatal("no request was made")
	}
	return r.calls[len(r.calls)-1]
}

// doc builds a normalized response from data and included entities.
func doc(t *testing.T, data map[string]any, included ...map[string]any) string {
	t.Helper()
	if included == nil {
		included = []map[string]any{}
	}
	return string(mustJSON(t, map[string]any{"data": data, "included": included}))
}
