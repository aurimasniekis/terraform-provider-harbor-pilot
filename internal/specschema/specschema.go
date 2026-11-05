// Package specschema fills explicit zero values into resource specs and
// canonicalizes JSON for comparison.
//
// The control-plane API requires every property of some specs to be present,
// including ones whose zero value is a sensible default (an empty
// description, no labels, no limits). Users write only what they mean; Fill
// adds the remaining required properties with zero values before a request.
// Properties named "enabled" are never filled: they must be explicit.
// Properties whose zero value the API would reject (an enum without "", a
// number with a positive minimum) are left out so the server names them.
//
// schemas.json is generated from api/openapi.yaml by tools/genschema.
package specschema

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
)

//go:generate go run ../../tools/genschema ../../api/openapi.yaml schemas.json

//go:embed schemas.json
var raw []byte

// Node is a simplified JSON schema.
type Node struct {
	Type     string           `json:"type,omitempty"`
	Required []string         `json:"required,omitempty"`
	Props    map[string]*Node `json:"props,omitempty"`
	Items    *Node            `json:"items,omitempty"`
	Map      bool             `json:"map,omitempty"`
	Enum     []string         `json:"enum,omitempty"`
	Minimum  *float64         `json:"minimum,omitempty"`
}

var (
	once    sync.Once
	schemas map[string]*Node
)

// Get returns the schema of a spec type (e.g. "AccountSpec").
func Get(name string) *Node {
	once.Do(func() {
		if err := json.Unmarshal(raw, &schemas); err != nil {
			panic(fmt.Sprintf("specschema: %v", err))
		}
	})
	return schemas[name]
}

// FillJSON parses a JSON object, fills it against the named schema and
// re-encodes it.
func FillJSON(schema string, in []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(in, &v); err != nil {
		return nil, err
	}
	if _, ok := v.(map[string]any); !ok {
		return nil, fmt.Errorf("spec must be a JSON object")
	}
	if n := Get(schema); n != nil {
		v = fill(n, v)
	}
	return json.Marshal(v)
}

func fill(n *Node, v any) any {
	switch {
	case n == nil:
		return v
	case n.Props != nil:
		m, ok := v.(map[string]any)
		if !ok {
			return v
		}
		for k, child := range n.Props {
			if cur, present := m[k]; present {
				m[k] = fill(child, cur)
				continue
			}
			if k == "enabled" || !slices.Contains(n.Required, k) {
				continue
			}
			if z, ok := zero(child); ok {
				m[k] = z
			}
		}
		return m
	case n.Items != nil:
		if arr, ok := v.([]any); ok {
			for i := range arr {
				arr[i] = fill(n.Items, arr[i])
			}
		}
	}
	return v
}

func zero(n *Node) (any, bool) {
	switch {
	case n.Props != nil:
		return fill(n, map[string]any{}), true
	case n.Map:
		return map[string]any{}, true
	}
	switch n.Type {
	case "string":
		if len(n.Enum) > 0 && !slices.Contains(n.Enum, "") {
			return nil, false
		}
		return "", true
	case "boolean":
		return false, true
	case "integer", "number":
		if n.Minimum != nil && *n.Minimum > 0 {
			return nil, false
		}
		return 0, true
	case "array":
		return []any{}, true
	case "object":
		return map[string]any{}, true
	}
	return nil, false
}

// Canonical re-encodes JSON with sorted object keys and no insignificant
// whitespace, so semantically equal documents compare equal.
func Canonical(in []byte) (string, error) {
	var v any
	if err := json.Unmarshal(in, &v); err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	return string(b), err
}
