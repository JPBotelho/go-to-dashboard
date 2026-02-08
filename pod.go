package main

import (
	"encoding/json"
	"os"
	"os/exec"
)

// PodData holds the fetched pod context from k9s + kubectl.
type PodData struct {
	Name      string
	Namespace string
	Labels    map[string]string
}

// NewPodData creates a PodData from CLI args. Labels are not yet fetched.
func NewPodData(name, namespace string) *PodData {
	if name == "" {
		return nil
	}
	return &PodData{
		Name:      name,
		Namespace: namespace,
	}
}

// FetchLabels calls kubectl to populate the pod's labels.
func (p *PodData) FetchLabels() error {
	args := []string{"get", "pod", p.Name, "-o", "json"}
	if p.Namespace != "" {
		args = append(args, "-n", p.Namespace)
	}

	cmd := exec.Command("kubectl", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	var pod struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(out, &pod); err != nil {
		return err
	}
	if pod.Metadata.Labels == nil {
		p.Labels = map[string]string{}
	} else {
		p.Labels = pod.Metadata.Labels
	}
	return nil
}

// FilterMenuItems returns only the menu items whose label filters match this pod.
// If PodData is nil (no pod context), items with filters are excluded and items without filters are kept.
func FilterMenuItems(items []MenuItem, pd *PodData) []MenuItem {
	var filtered []MenuItem
	for _, item := range items {
		if pd == nil {
			// No pod context: only show items with no filters
			if len(item.Filters.MustHaveLabels) == 0 {
				filtered = append(filtered, item)
			}
			continue
		}
		if item.MatchesLabels(pd.Labels) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
