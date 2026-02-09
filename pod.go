package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// PodData holds the fetched pod context from k9s + kubectl.
type PodData struct {
	Name      string
	Namespace string
	RawJSON   []byte                 // full kubectl JSON output
	Parsed    map[string]interface{} // unmarshaled for path traversal
}

// NewPodData creates a PodData from CLI args. JSON is not yet fetched.
func NewPodData(name, namespace string) *PodData {
	if name == "" {
		return nil
	}
	return &PodData{
		Name:      name,
		Namespace: namespace,
	}
}

// FetchPodJSON calls kubectl to populate the pod's full JSON.
func (p *PodData) FetchPodJSON() error {
	args := []string{"get", "pod", p.Name, "-o", "json"}
	if p.Namespace != "" {
		args = append(args, "-n", p.Namespace)
	}

	cmd := exec.Command("kubectl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	p.RawJSON = out
	var parsed map[string]interface{}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return err
	}
	p.Parsed = parsed
	return nil
}

// ResolvePath walks the parsed JSON using a dot-separated path and returns
// whatever value lives at that location (map, slice, string, number, etc.).
func (p *PodData) ResolvePath(path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	var current interface{} = p.Parsed
	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// Labels is a convenience method that extracts metadata.labels as map[string]string.
func (p *PodData) Labels() map[string]string {
	val, ok := p.ResolvePath("metadata.labels")
	if !ok {
		return map[string]string{}
	}
	m, ok := val.(map[string]interface{})
	if !ok {
		return map[string]string{}
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = stringify(v)
	}
	return result
}

// FlattenPaths returns all dot-notation paths and their values from the parsed JSON,
// sorted alphabetically. Each entry is "path = value".
func (p *PodData) FlattenPaths() []string {
	var result []string
	flattenRecurse("", p.Parsed, &result)
	sort.Strings(result)
	return result
}

func flattenRecurse(prefix string, val interface{}, out *[]string) {
	switch v := val.(type) {
	case map[string]interface{}:
		for k, child := range v {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			flattenRecurse(path, child, out)
		}
	case []interface{}:
		for i, child := range v {
			path := fmt.Sprintf("%s.%d", prefix, i)
			flattenRecurse(path, child, out)
		}
	default:
		*out = append(*out, fmt.Sprintf("%s = %s", prefix, stringify(val)))
	}
}

// stringify converts an arbitrary JSON value to its string representation.
func stringify(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		return fmt.Sprintf("%t", val)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}

// FilterMenuItems returns only the menu items whose conditions match this pod.
// If PodData is nil (no pod context), items with conditions are excluded and
// items without conditions are kept.
func FilterMenuItems(items []MenuItem, pd *PodData) []MenuItem {
	var filtered []MenuItem
	for _, item := range items {
		if pd == nil {
			// No pod context: only show items with no conditions
			if len(item.Filters.Conditions) == 0 {
				filtered = append(filtered, item)
			}
			continue
		}
		if item.MatchesPod(pd) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
