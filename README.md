# go-to-dashboard

A k9s plugin that shows a filterable menu of dashboards/links via fzf. Select one and it opens in your browser, with the URL dynamically built from the selected pod's labels.

## Features

- Menu items defined in `config.json` (loaded at runtime, no rebuild needed)
- Items can be **filtered by arbitrary pod fields** using dot-notation paths and regex patterns (labels, annotations, status, node name, etc.)
- **Negative filters** supported via `invert` (e.g. "must NOT have annotation X")
- URLs can have **template variables** that inject any pod field value (e.g. Datadog `tpl_var_*` params, node names, pod names)
- Preview pane shows the resolved URL with color-coded template variable segments, pod info, and all labels
- `--debug` flag adds a menu option to inspect all available pod spec paths
- Cross-platform URL opening (WSL, Linux, macOS, Windows)

## Build

```bash
cd ~/.config/k9s/go-to-dashboard
go build -o go-to-dashboard .
```

## Requirements

- **fzf** on PATH
- **kubectl** on PATH (to fetch pod JSON)

## k9s plugin config

In `~/.config/k9s/plugins.yaml`:

```yaml
plugins:
  go-to-dashboard:
    shortCut: Ctrl-L
    description: Go to Dashboard
    confirm: true
    scopes:
      - pods
    command: bash
    background: false
    args:
      - -c
      - 'exec "$HOME/.config/k9s/go-to-dashboard/go-to-dashboard" -pod "$NAME" -namespace "$NAMESPACE"'
```

## Config

`config.json` sits next to the binary. Each menu item has:

| Field | Required | Description |
|-------|----------|-------------|
| `title` | yes | Text shown in the fzf list |
| `description` | yes | Shown in the fzf preview pane |
| `url` | yes | Base URL to open |
| `filters.conditions` | no | Only show this item if the pod matches all conditions |
| `templateVars` | no | Append to the URL based on pod field values |

### Filters

Each entry in `conditions` matches against the pod's JSON using dot-notation paths:

| Field | Default | Description |
|-------|---------|-------------|
| `path` | **required** | Dot-notation path into the pod JSON (e.g. `metadata.labels`, `spec.nodeName`) |
| `keyPattern` | `.*` | Regex for map keys (only for map fields like labels/annotations). Implicitly anchored with `^...$` |
| `valuePattern` | `.*` | Regex for values. Implicitly anchored with `^...$` |
| `invert` | `false` | Negate the condition (e.g. "must NOT have") |

Patterns are implicitly anchored, so `app` matches exactly `app`, not `myapp`. Use `app.*` for prefix matching, `.*team.*` for substring matching.

Examples:

- `{ "path": "metadata.labels", "keyPattern": "app" }` — pod must have the `app` label (any value)
- `{ "path": "metadata.labels", "keyPattern": "app", "valuePattern": "nginx" }` — pod must have `app=nginx`
- `{ "path": "status.phase", "valuePattern": "Running" }` — pod must be Running
- `{ "path": "spec.nodeName", "valuePattern": "prod-.*" }` — pod must be on a prod node
- `{ "path": "metadata.annotations", "keyPattern": "internal\\.skip", "invert": true }` — pod must NOT have the annotation

All conditions are ANDed together. Items with no conditions always appear.

### Template variables

Each entry in `templateVars` has:

| Field | Required | Description |
|-------|----------|-------------|
| `path` | yes | Dot-notation path to a value (e.g. `metadata.labels.app`, `spec.nodeName`) |
| `urlAppend` | yes | String appended to the URL; `$VALUE` is replaced with the resolved value |

If the path doesn't resolve, that `urlAppend` is skipped.

### Debug mode

Pass `--debug` to add a `[DEBUG]` option at the top of the fzf menu that shows all available dot-notation paths for the current pod (copied to clipboard and opened in VS Code). Useful for discovering which paths to use in conditions and templateVars.

### Example

```json
{
  "menuItems": [
    {
      "description": "Datadog dashboard filtered by app label",
      "title": "Datadog App Dashboard",
      "url": "https://app.datadoghq.com/dashboard/abc-123",
      "filters": {
        "conditions": [
          { "path": "metadata.labels", "keyPattern": "app" }
        ]
      },
      "templateVars": [
        { "path": "metadata.labels.app", "urlAppend": "?tpl_var_app=$VALUE" },
        { "path": "spec.nodeName", "urlAppend": "&host=$VALUE" }
      ]
    },
    {
      "description": "Only shows for pods with app=nginx and env=production",
      "title": "Prod Nginx Logs",
      "url": "https://grafana.example.com/nginx",
      "filters": {
        "conditions": [
          { "path": "metadata.labels", "keyPattern": "app", "valuePattern": "nginx" },
          { "path": "metadata.labels", "keyPattern": "env", "valuePattern": "production" }
        ]
      }
    },
    {
      "description": "Always shows (no filters)",
      "title": "Google",
      "url": "https://google.com"
    }
  ]
}
```

For a pod with `app=nginx` on node `prod-pool-node-01`, selecting "Datadog App Dashboard" opens:

```
https://app.datadoghq.com/dashboard/abc-123?tpl_var_app=nginx&host=prod-pool-node-01
```
