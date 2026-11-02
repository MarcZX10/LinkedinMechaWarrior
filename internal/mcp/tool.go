package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Tool is one operation offered to the assistant.
type Tool struct {
	Name        string
	Title       string
	Description string
	Params      []Param
	// ReadOnly tools only read. The others change something on LinkedIn and
	// are only offered when the server allows writing.
	ReadOnly bool
	// Destructive tools remove something (a post, a connection).
	Destructive bool
	// Local tools change only lmw's own settings on this computer (watches,
	// notification channels), never LinkedIn, so they are always offered.
	Local bool
	// Confirm describes what a call that changes something will do. When the
	// client supports elicitation, the person must accept it first.
	Confirm func(Args) (string, error)
	Run     func(context.Context, Args) (any, error)
}

// Param is one tool argument.
type Param struct {
	Name        string
	Type        string // "string", "integer" or "boolean"
	Description string
	Required    bool
	Enum        []string
	Default     any
	// Min and Max bound integers when Max is not zero.
	Min, Max int
}

// Args are the arguments of one call, checked against the tool's Params and
// with defaults filled in. Integers are ints.
type Args map[string]any

// String returns a string argument, or "".
func (a Args) String(name string) string { s, _ := a[name].(string); return s }

// Int returns an integer argument, or 0.
func (a Args) Int(name string) int { n, _ := a[name].(int); return n }

// Bool returns a boolean argument, or false.
func (a Args) Bool(name string) bool { b, _ := a[name].(bool); return b }

func (t Tool) describe() map[string]any {
	properties := map[string]any{}
	required := []string{}
	for _, p := range t.Params {
		property := map[string]any{"type": p.Type, "description": p.Description}
		if len(p.Enum) > 0 {
			property["enum"] = p.Enum
		}
		if p.Default != nil {
			property["default"] = p.Default
		}
		if p.Max != 0 {
			property["minimum"], property["maximum"] = p.Min, p.Max
		}
		properties[p.Name] = property
		if p.Required {
			required = append(required, p.Name)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return map[string]any{
		"name":        t.Name,
		"title":       t.Title,
		"description": t.Description,
		"inputSchema": schema,
		"annotations": map[string]any{
			"title":           t.Title,
			"readOnlyHint":    t.ReadOnly,
			"destructiveHint": t.Destructive,
			"openWorldHint":   true,
		},
	}
}

// check validates the arguments of a call. Its errors are meant for the
// assistant, which can correct the call.
func (t Tool) check(raw map[string]any) (Args, error) {
	args := Args{}
	known := map[string]bool{}
	for _, p := range t.Params {
		known[p.Name] = true
		value, present := raw[p.Name]
		if !present || value == nil {
			switch {
			case p.Required:
				return nil, fmt.Errorf("missing required argument %q", p.Name)
			case p.Default != nil:
				args[p.Name] = p.Default
			}
			continue
		}
		checked, err := p.check(value)
		if err != nil {
			return nil, err
		}
		args[p.Name] = checked
	}
	var unknown []string
	for name := range raw {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown argument %s for %s", strings.Join(unknown, ", "), t.Name)
	}
	return args, nil
}

func (p Param) check(value any) (any, error) {
	switch p.Type {
	case "string":
		s, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("argument %q must be a string", p.Name)
		}
		if len(p.Enum) > 0 && !contains(p.Enum, s) {
			return nil, fmt.Errorf("argument %q must be one of: %s", p.Name, strings.Join(p.Enum, ", "))
		}
		return s, nil
	case "integer":
		f, ok := value.(float64)
		if !ok || f != float64(int(f)) {
			return nil, fmt.Errorf("argument %q must be a whole number", p.Name)
		}
		n := int(f)
		if p.Max != 0 && (n < p.Min || n > p.Max) {
			return nil, fmt.Errorf("argument %q must be between %d and %d", p.Name, p.Min, p.Max)
		}
		return n, nil
	case "boolean":
		b, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("argument %q must be true or false", p.Name)
		}
		return b, nil
	}
	return value, nil
}

func contains(items []string, item string) bool {
	for _, v := range items {
		if v == item {
			return true
		}
	}
	return false
}
