package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type LabelSelector struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

type ItemFilters struct {
	MustHaveLabels []LabelSelector `json:"mustHaveLabels,omitempty"`
}

type TemplateVar struct {
	Label     string `json:"label"`     // pod label key to look up
	URLAppend string `json:"urlAppend"` // string to append to URL; $LABEL_VALUE is replaced with the label's value
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

// LoadConfig reads the JSON file at path, unmarshals it, and validates.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if err := ValidateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ValidateConfig checks that every MenuItem has a non-empty Title and URL,
// and every LabelSelector has a non-empty Key.
func ValidateConfig(cfg Config) error {
	if len(cfg.MenuItems) == 0 {
		return fmt.Errorf("config: no menu items")
	}
	for i, item := range cfg.MenuItems {
		if item.Title == "" {
			return fmt.Errorf("config: menuItems[%d] has empty title", i)
		}
		if item.URL == "" {
			return fmt.Errorf("config: menuItems[%d] (%s) has empty url", i, item.Title)
		}
		for j, sel := range item.Filters.MustHaveLabels {
			if sel.Key == "" {
				return fmt.Errorf("config: menuItems[%d] (%s) mustHaveLabels[%d] has empty key", i, item.Title, j)
			}
		}
		for j, tv := range item.TemplateVars {
			if tv.Label == "" {
				return fmt.Errorf("config: menuItems[%d] (%s) templateVars[%d] has empty label", i, item.Title, j)
			}
			if tv.URLAppend == "" {
				return fmt.Errorf("config: menuItems[%d] (%s) templateVars[%d] has empty urlAppend", i, item.Title, j)
			}
		}
	}
	return nil
}

// ResolveURL returns the item's URL with templateVars applied.
// For each templateVar, if the label exists in podLabels, urlAppend is appended
// to the URL with $LABEL_VALUE replaced by the label's value.
func (item MenuItem) ResolveURL(podLabels map[string]string) string {
	url := item.URL
	for _, tv := range item.TemplateVars {
		val, exists := podLabels[tv.Label]
		if !exists {
			continue
		}
		appendStr := strings.ReplaceAll(tv.URLAppend, "$LABEL_VALUE", val)
		url += appendStr
	}
	return url
}

// MatchesLabels returns true if podLabels satisfy all MustHaveLabels filters.
// If MustHaveLabels is empty, always returns true.
func (item MenuItem) MatchesLabels(podLabels map[string]string) bool {
	for _, sel := range item.Filters.MustHaveLabels {
		val, exists := podLabels[sel.Key]
		if !exists {
			return false
		}
		if sel.Value != "" && val != sel.Value {
			return false
		}
	}
	return true
}
