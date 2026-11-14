// Command genschema extracts the spec schemas of every managed kind from
// the vendored OpenAPI document into internal/specschema/schemas.json. The
// provider uses them to send explicit zero values for fields the API marks
// required but that users may reasonably omit.
//
//	go run ./tools/genschema api/openapi.yaml internal/specschema/schemas.json internal/specschema/enums.json
//
// The third file holds the enums the provider validates by hand: the
// permissions a service principal can hold and the OIDC provider enums.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Node mirrors internal/specschema.Node.
type Node struct {
	Type     string           `json:"type,omitempty"`
	Required []string         `json:"required,omitempty"`
	Props    map[string]*Node `json:"props,omitempty"`
	Items    *Node            `json:"items,omitempty"`
	Map      bool             `json:"map,omitempty"`
	Enum     []string         `json:"enum,omitempty"`
	Minimum  *float64         `json:"minimum,omitempty"`
}

type doc struct {
	Paths      map[string]map[string]any `yaml:"paths"`
	Components struct {
		Schemas map[string]map[string]any `yaml:"schemas"`
	} `yaml:"components"`
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: genschema OPENAPI_YAML SCHEMAS_JSON ENUMS_JSON")
		os.Exit(2)
	}
	b, err := os.ReadFile(os.Args[1]) //nolint:gosec // developer tool input
	if err != nil {
		panic(err)
	}
	var d doc
	if err := yaml.Unmarshal(b, &d); err != nil {
		panic(err)
	}
	out := map[string]*Node{}
	for name := range d.Components.Schemas {
		if spec, ok := strings.CutPrefix(name, "WriteBody"); ok {
			out[spec] = resolve(d, spec, 0)
		}
	}
	out["PlanDocument"] = resolve(d, "PlanDocument", 0)
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	enc, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(os.Args[2], append(enc, '\n'), 0o644); err != nil { //nolint:gosec // generated source file
		panic(err)
	}
	fmt.Println("schemas:", strings.Join(keys, ", "))

	// RoleMapping.role has no enum of its own: it takes the built-in roles,
	// which default_role lists (plus "" for none).
	enums := map[string][]string{
		"permissions":       permissions(d),
		"oidc_role":         withoutEmpty(enumAt(d, "OidcBody", "default_role")),
		"oidc_default_role": enumAt(d, "OidcBody", "default_role"),
		"oidc_provisioning": enumAt(d, "OidcBody", "provisioning"),
	}
	enc, err = json.MarshalIndent(enums, "", " ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(os.Args[3], append(enc, '\n'), 0o644); err != nil { //nolint:gosec // generated source file
		panic(err)
	}
}

// permissions returns every permission the server knows: the x-permission of
// each operation (except the public and session markers) plus the explicitly
// grantable trace content permissions (GrantsInBody), in server order.
func permissions(d doc) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && p != "public" && p != "session" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, item := range d.Paths {
		for _, op := range item {
			if m, ok := op.(map[string]any); ok {
				p, _ := m["x-permission"].(string)
				add(p)
			}
		}
	}
	for _, p := range enumAt(d, "GrantsInBody", "grants", "items") {
		add(p)
	}
	sort.Strings(out)
	return out
}

// enumAt returns the enum found by following properties (and "items") from
// a component schema.
func enumAt(d doc, schema string, path ...string) []string {
	cur := d.Components.Schemas[schema]
	for _, p := range path {
		if p == "items" {
			cur, _ = cur["items"].(map[string]any)
		} else {
			props, _ := cur["properties"].(map[string]any)
			cur, _ = props[p].(map[string]any)
		}
		if cur == nil {
			panic("genschema: no " + strings.Join(path, ".") + " in " + schema)
		}
	}
	var out []string
	for _, x := range cur["enum"].([]any) {
		out = append(out, x.(string))
	}
	return out
}

func resolve(d doc, name string, depth int) *Node {
	s, ok := d.Components.Schemas[name]
	if !ok {
		panic("missing schema " + name)
	}
	return node(d, s, depth)
}

func node(d doc, s map[string]any, depth int) *Node {
	if depth > 12 {
		return &Node{}
	}
	if ref, ok := s["$ref"].(string); ok {
		return resolve(d, strings.TrimPrefix(ref, "#/components/schemas/"), depth+1)
	}
	n := &Node{}
	switch t := s["type"].(type) {
	case string:
		n.Type = t
	case []any: // ["array","null"] style
		for _, x := range t {
			if xs, _ := x.(string); xs != "null" {
				n.Type = xs
			}
		}
	}
	if req, ok := s["required"].([]any); ok {
		for _, r := range req {
			n.Required = append(n.Required, r.(string))
		}
	}
	if props, ok := s["properties"].(map[string]any); ok {
		n.Props = map[string]*Node{}
		for k, v := range props {
			if k == "$schema" {
				continue
			}
			n.Props[k] = node(d, v.(map[string]any), depth+1)
		}
	}
	if items, ok := s["items"].(map[string]any); ok {
		n.Items = node(d, items, depth+1)
	}
	if ap, ok := s["additionalProperties"]; ok {
		if m, ok := ap.(map[string]any); ok && len(m) >= 0 && n.Props == nil {
			n.Map = true
		}
		if b, ok := ap.(bool); ok && b && n.Props == nil {
			n.Map = true
		}
	}
	if n.Type == "object" && n.Props == nil {
		n.Map = true
	}
	if e, ok := s["enum"].([]any); ok {
		for _, x := range e {
			if xs, ok := x.(string); ok {
				n.Enum = append(n.Enum, xs)
			}
		}
	}
	switch m := s["minimum"].(type) {
	case int:
		f := float64(m)
		n.Minimum = &f
	case float64:
		n.Minimum = &m
	}
	return n
}

func withoutEmpty(xs []string) []string {
	var out []string
	for _, x := range xs {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}
