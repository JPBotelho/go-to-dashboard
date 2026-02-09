package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Condition describes a single filter check against a pod's JSON fields.
// Patterns are implicitly anchored with ^...$ before compilation.
type Condition struct {
	Path         string `json:"path"`
	KeyPattern   string `json:"keyPattern,omitempty"`
	ValuePattern string `json:"valuePattern,omitempty"`
	Invert       bool   `json:"invert,omitempty"`

	// compiled regexes (populated by ValidateConfig, not serialized)
	keyRe   *regexp.Regexp
	valueRe *regexp.Regexp
}

type ItemFilters struct {
	Conditions []Condition `json:"conditions,omitempty"`
}

// TemplateVar extracts a value from the pod JSON and appends it to the URL.
// $VALUE in urlAppend is replaced with the resolved value.
type TemplateVar struct {
	Path      string `json:"path"`      // dot-notation path into pod JSON (e.g. "metadata.labels.app", "spec.nodeName")
	URLAppend string `json:"urlAppend"` // string appended to URL; $VALUE is replaced with the resolved value
}

type MenuItem struct {
	Description  string        `json:"description"`
	Title        string        `json:"title"`
	URL          string        `json:"url"`
	Filters      ItemFilters   `json:"filters,omitempty"`
	TemplateVars []TemplateVar `json:"templateVars,omitempty"`
}

type Config struct {
	MenuItems []MenuItem `json:"menuItems"`
}

// anchorPattern wraps a pattern in ^...$ if not already anchored.
func anchorPattern(p string) string {
	if !strings.HasPrefix(p, "^") {
		p = "^" + p
	}
	if !strings.HasSuffix(p, "$") {
		p = p + "$"
	}
	return p
}

// LoadConfig reads the JSON file at path, unmarshals it, and validates + compiles.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if err := ValidateConfig(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ValidateConfig checks that every MenuItem has a non-empty Title and URL,
// validates and compiles regex patterns in conditions and templateVars.
func ValidateConfig(cfg *Config) error {
	if len(cfg.MenuItems) == 0 {
		return fmt.Errorf("config: no menu items")
	}
	for i := range cfg.MenuItems {
		item := &cfg.MenuItems[i]
		if item.Title == "" {
			return fmt.Errorf("config: menuItems[%d] has empty title", i)
		}
		if item.URL == "" {
			return fmt.Errorf("config: menuItems[%d] (%s) has empty url", i, item.Title)
		}
		for j := range item.Filters.Conditions {
			cond := &item.Filters.Conditions[j]
			if cond.Path == "" {
				return fmt.Errorf("config: menuItems[%d] (%s) conditions[%d] has empty path", i, item.Title, j)
			}
			// Default patterns
			if cond.KeyPattern == "" {
				cond.KeyPattern = ".*"
			}
			if cond.ValuePattern == "" {
				cond.ValuePattern = ".*"
			}
			// Compile with implicit anchoring
			keyRe, err := regexp.Compile(anchorPattern(cond.KeyPattern))
			if err != nil {
				return fmt.Errorf("config: menuItems[%d] (%s) conditions[%d] invalid keyPattern %q: %w", i, item.Title, j, cond.KeyPattern, err)
			}
			cond.keyRe = keyRe
			valueRe, err := regexp.Compile(anchorPattern(cond.ValuePattern))
			if err != nil {
				return fmt.Errorf("config: menuItems[%d] (%s) conditions[%d] invalid valuePattern %q: %w", i, item.Title, j, cond.ValuePattern, err)
			}
			cond.valueRe = valueRe
		}
		for j, tv := range item.TemplateVars {
			if tv.Path == "" {
				return fmt.Errorf("config: menuItems[%d] (%s) templateVars[%d] has empty path", i, item.Title, j)
			}
			if tv.URLAppend == "" {
				return fmt.Errorf("config: menuItems[%d] (%s) templateVars[%d] has empty urlAppend", i, item.Title, j)
			}
		}
	}
	return nil
}

// Evaluate checks whether this condition matches the given pod data.
func (c *Condition) Evaluate(pd *PodData) bool {
	val, ok := pd.ResolvePath(c.Path)

	var matched bool
	if !ok || val == nil {
		matched = false
	} else {
		switch v := val.(type) {
		case map[string]interface{}:
			matched = c.matchMap(v)
		case []interface{}:
			matched = c.matchArray(v)
		default:
			matched = c.valueRe.MatchString(stringify(val))
		}
	}

	if c.Invert {
		return !matched
	}
	return matched
}

// matchMap returns true if at least one map entry has a key matching keyRe
// and a value matching valueRe.
func (c *Condition) matchMap(m map[string]interface{}) bool {
	for k, v := range m {
		if c.keyRe.MatchString(k) && c.valueRe.MatchString(stringify(v)) {
			return true
		}
	}
	return false
}

// matchArray returns true if at least one element (stringified) matches valueRe.
func (c *Condition) matchArray(arr []interface{}) bool {
	for _, v := range arr {
		if c.valueRe.MatchString(stringify(v)) {
			return true
		}
	}
	return false
}

// MatchesPod returns true if all conditions in this item's filters pass.
// If there are no conditions, always returns true.
func (item MenuItem) MatchesPod(pd *PodData) bool {
	for i := range item.Filters.Conditions {
		if !item.Filters.Conditions[i].Evaluate(pd) {
			return false
		}
	}
	return true
}

// ResolveURL returns the item's URL with templateVars applied using pod data.
func (item MenuItem) ResolveURL(pd *PodData) string {
	url := item.URL
	for _, tv := range item.TemplateVars {
		val := tv.resolve(pd)
		if val == "" {
			continue
		}
		appendStr := strings.ReplaceAll(tv.URLAppend, "$VALUE", val)
		url += appendStr
	}
	return url
}

// resolve extracts the value for this template var from the pod data.
func (tv TemplateVar) resolve(pd *PodData) string {
	if pd == nil {
		return ""
	}
	val, ok := pd.ResolvePath(tv.Path)
	if !ok {
		return ""
	}
	return stringify(val)
}
