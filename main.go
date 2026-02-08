package main

import (
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pkg/browser"
)

// 10 ANSI colors with good contrast on dark backgrounds
var labelColors = []string{
	"\033[38;5;204m", // pink
	"\033[38;5;114m", // green
	"\033[38;5;209m", // orange
	"\033[38;5;75m",  // blue
	"\033[38;5;219m", // magenta
	"\033[38;5;186m", // yellow
	"\033[38;5;80m",  // cyan
	"\033[38;5;147m", // lavender
	"\033[38;5;174m", // salmon
	"\033[38;5;115m", // teal
}

const colorReset = "\033[0m"

// colorForKey returns a consistent color for a label key by hashing it.
func colorForKey(key string) string {
	h := fnv.New32a()
	h.Write([]byte(key))
	return labelColors[h.Sum32()%uint32(len(labelColors))]
}

func main() {
	pod := flag.String("pod", "", "pod name (from k9s)")
	namespace := flag.String("namespace", "", "namespace (from k9s)")
	flag.Parse()

	configPath := "config.json"
	if exe, err := os.Executable(); err == nil {
		configPath = filepath.Join(filepath.Dir(exe), "config.json")
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config! %v\n", err)
		time.Sleep(5 * time.Second)
		os.Exit(1)
	}

	// Build pod context and fetch labels
	pd := NewPodData(*pod, *namespace)
	if pd != nil {
		if err := pd.FetchLabels(); err != nil {
			fmt.Fprintf(os.Stderr, "kubectl get pod: %v\n", err)
		}
	}

	// Filter menu items based on pod labels
	items := FilterMenuItems(cfg.MenuItems, pd)
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "no menu items match this pod's labels")
	}

	// Build fzf input: "title\tdescription\turl" — resolve templateVars into URLs
	var podLabels map[string]string
	if pd != nil {
		podLabels = pd.Labels
	}
	var lines []string
	for _, it := range items {
		url := it.ResolveURL(podLabels)
		lines = append(lines, it.Title+"\t"+it.Description+"\t"+url)
	}
	input := strings.Join(lines, "\n")

	header := "Open a dashboard"
	if pd != nil {
		if pd.Namespace != "" {
			header = fmt.Sprintf("Open a dashboard — pod: %s (%s)", pd.Name, pd.Namespace)
		} else {
			header = fmt.Sprintf("Open a dashboard — pod: %s", pd.Name)
		}
	}

	// Write per-item preview files: split labels into "Filtered by" and "Not filtered by"
	var previewDir string
	previewCmd := `echo {2}; echo; echo "── URL ──"; echo; echo "  {3}"`
	if pd != nil && pd.Labels != nil {
		tmpDir, err := os.MkdirTemp("", "fzf-pod-preview-*")
		if err == nil {
			previewDir = tmpDir
			defer os.RemoveAll(previewDir)

			for i, it := range items {
				// Collect label keys used by this item's templateVars
				usedKeys := map[string]bool{}
				for _, tv := range it.TemplateVars {
					usedKeys[tv.Label] = true
				}

				// Sort all label keys
				allKeys := make([]string, 0, len(pd.Labels))
				for k := range pd.Labels {
					allKeys = append(allKeys, k)
				}
				sort.Strings(allKeys)

				// Build "Filtered by" (labels used in templateVars) and "All Pod Labels"
				var filtered []string
				for _, k := range allKeys {
					if usedKeys[k] {
						color := colorForKey(k)
						filtered = append(filtered, fmt.Sprintf("  %s%s = %s%s", color, k, pd.Labels[k], colorReset))
					}
				}

				var allLabels []string
				for _, k := range allKeys {
					color := colorForKey(k)
					allLabels = append(allLabels, fmt.Sprintf("  %s%s = %s%s", color, k, pd.Labels[k], colorReset))
				}

				fpath := filepath.Join(previewDir, fmt.Sprintf("%d.txt", i))
				f, err := os.Create(fpath)
				if err != nil {
					continue
				}
				if len(filtered) > 0 {
					fmt.Fprintf(f, "── Page will be scoped to ──\n\n")
					for _, l := range filtered {
						fmt.Fprintln(f, l)
					}
					fmt.Fprintln(f)
				}
				fmt.Fprintf(f, "── All Pod Labels ──\n\n")
				for _, l := range allLabels {
					fmt.Fprintln(f, l)
				}
				f.Close()
			}
			previewCmd = fmt.Sprintf(`echo {2}; echo; echo "── URL ──"; echo; echo "  {3}"; echo; cat "%s/{n}.txt" 2>/dev/null`, previewDir)
		}
	}

	// fzf: list shows title (col 1), sidebar preview shows description + labels, selection returns full line so we get url (col 3)
	cmd := exec.Command("fzf",
		"--style", "full",
		"--ansi",
		"--header="+header,
		"--with-nth", "1",
		"--delimiter=\t",
		"--preview="+previewCmd,
		"--preview-window=right",
	)
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok && e.ExitCode() == 130 {
			fmt.Fprintf(os.Stderr, "fzf: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "fzf: %v\n", err)
		}
	}
	selected := strings.TrimSpace(string(out))
	if selected == "" {
		fmt.Fprintf(os.Stderr, "no selection\n")
	}
	parts := strings.Split(selected, "\t")
	if len(parts) < 3 {
		fmt.Fprintf(os.Stderr, "invalid selection\n")
	}

	url := strings.TrimSpace(parts[len(parts)-1])
	if err := openURL(url); err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
	}
}

// openURL tries Windows (WSL) first to avoid xdg-open spam, then pkg/browser.
func openURL(url string) error {
	if data, err := os.ReadFile("/proc/version"); err == nil && strings.Contains(strings.ToLower(string(data)), "microsoft") {
		c := exec.Command("cmd.exe", "/c", "start", "", url)
		if c.Run() == nil {
			return nil
		}
	}
	return browser.OpenURL(url)
}
