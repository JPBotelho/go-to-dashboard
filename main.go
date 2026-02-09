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
	"golang.design/x/clipboard"
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
	debug := flag.Bool("debug", false, "show DEBUG option to inspect pod spec paths")
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

	// Build pod context and fetch full JSON
	var podErr string
	pd := NewPodData(*pod, *namespace)
	if pd != nil {
		if err := pd.FetchPodJSON(); err != nil {
			podErr = fmt.Sprintf("kubectl get pod: %v", err)
			fmt.Fprintf(os.Stderr, "%s\n", podErr)
		}
	}

	// Filter menu items based on pod conditions
	items := FilterMenuItems(cfg.MenuItems, pd)
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "no menu items match this pod")
	}

	// Build fzf input: "title\tdescription\turl" — resolve templateVars into URLs
	const debugMarker = "__DEBUG_POD_SPEC__"
	var lines []string
	// Add DEBUG entry at the top when --debug and pod data is available
	if *debug && pd != nil && pd.Parsed != nil {
		lines = append(lines, "[DEBUG] Open pod spec paths in VS Code\tAll dot-notation paths and values for this pod\t"+debugMarker)
	}
	for _, it := range items {
		url := it.ResolveURL(pd)
		desc := it.Description
		if pd != nil {
			podDesc := pd.Name
			if pd.Namespace != "" {
				podDesc = pd.Namespace + "/" + pd.Name
			}
			desc = "[" + podDesc + "] " + desc
		}
		lines = append(lines, it.Title+"\t"+desc+"\t"+url)
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
	if podErr != "" {
		header += fmt.Sprintf("\n⚠ ERROR: %s", podErr)
	}

	// Write per-item preview files showing scoped templateVars and all pod labels
	var previewDir string
	previewCmd := `echo {2}; echo; echo "── URL ──"; echo; echo "  {3}"`
	if pd != nil && pd.Parsed != nil {
		podLabels := pd.Labels()
		tmpDir, err := os.MkdirTemp("", "fzf-pod-preview-*")
		if err == nil {
			previewDir = tmpDir
			defer os.RemoveAll(previewDir)

			for i, it := range items {
				// Collect resolved templateVar info
				type tvResolved struct {
					path, value, appended string
				}
				var resolved []tvResolved
				for _, tv := range it.TemplateVars {
					val := tv.resolve(pd)
					if val == "" {
						continue
					}
					appended := strings.ReplaceAll(tv.URLAppend, "$VALUE", val)
					resolved = append(resolved, tvResolved{tv.Path, val, appended})
				}

				// Build colored URL: base URL plain, each templateVar append colored
				coloredURL := "  " + it.URL
				for _, r := range resolved {
					color := colorForKey(r.path)
					coloredURL += fmt.Sprintf("%s%s%s", color, r.appended, colorReset)
				}

				// Sort all label keys
				allKeys := make([]string, 0, len(podLabels))
				for k := range podLabels {
					allKeys = append(allKeys, k)
				}
				sort.Strings(allKeys)

				var allLabelLines []string
				for _, k := range allKeys {
					color := colorForKey(k)
					allLabelLines = append(allLabelLines, fmt.Sprintf("  %s%s = %s%s", color, k, podLabels[k], colorReset))
				}

				fpath := filepath.Join(previewDir, fmt.Sprintf("%d.txt", i))
				f, err := os.Create(fpath)
				if err != nil {
					continue
				}
				// URL section with colored templateVar segments
				fmt.Fprintf(f, "── URL ──\n\n")
				fmt.Fprintln(f, coloredURL)
				if len(resolved) > 0 {
					fmt.Fprintln(f)
					for _, r := range resolved {
						color := colorForKey(r.path)
						fmt.Fprintf(f, "  %s%s%s = %s\n", color, r.path, colorReset, r.value)
					}
				}
				fmt.Fprintln(f)
				// Pod info section
				fmt.Fprintf(f, "── Pod Info ──\n\n")
				podName, _ := pd.ResolvePath("metadata.name")
				nodeName, _ := pd.ResolvePath("spec.nodeName")
				fmt.Fprintf(f, "  %spod%s  = %s\n", colorForKey("pod"), colorReset, stringify(podName))
				fmt.Fprintf(f, "  %snode%s = %s\n", colorForKey("node"), colorReset, stringify(nodeName))
				for _, l := range allLabelLines {
					fmt.Fprintln(f, l)
				}
				f.Close()
			}
			previewCmd = fmt.Sprintf(`echo {2}; echo; cat "%s/{n}.txt" 2>/dev/null`, previewDir)
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
	if url == debugMarker {
		// Pipe flattened pod paths into VS Code via stdin
		paths := pd.FlattenPaths()
		content := strings.Join(paths, "\n") + "\n"
		// Copy to clipboard
		if err := clipboard.Init(); err != nil {
			fmt.Fprintf(os.Stderr, "clipboard init: %v\n", err)
		} else {
			clipboard.Write(clipboard.FmtText, []byte(content))
			fmt.Fprintf(os.Stderr, "Copied %d paths to clipboard\n", len(paths))
		}
		// Also try opening in VS Code
		codeCmd := exec.Command("code", "-")
		codeCmd.Stdin = strings.NewReader(content)
		codeCmd.Stdout = os.Stdout
		codeCmd.Stderr = os.Stderr
		if err := codeCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "code -: %v\n", err)
		}
		time.Sleep(5 * time.Second)
		return
	}
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
