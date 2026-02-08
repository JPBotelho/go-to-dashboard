# go-to-dashboard

A k9s plugin that shows a filterable menu of dashboards/links via fzf. Select one and it opens in your browser, with the URL dynamically built from the selected pod's labels.

## Features

- Menu items defined in `config.json` (loaded at runtime, no rebuild needed)
- Items can be **filtered by pod labels** so only relevant dashboards appear
- URLs can have **template variables** that inject pod label values (e.g. Datadog `tpl_var_*` params)
- Preview pane shows the item description, resolved URL, filtered-by labels, and all pod labels (color-coded)
- Cross-platform URL opening (WSL, Linux, macOS, Windows)

## Build

```bash
cd ~/.config/k9s/go-to-dashboard
go build -o go-to-dashboard .
```

## Requirements

- **fzf** on PATH
- **kubectl** on PATH (to fetch pod labels)

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
| `filters.mustHaveLabels` | no | Only show this item if the pod has these labels |
| `templateVars` | no | Append to the URL based on pod label values |

### Filters

Each entry in `mustHaveLabels` has a `key` and an optional `value`:

- `{ "key": "app" }` — pod must have the `app` label (any value)
- `{ "key": "env", "value": "production" }` — pod must have `env=production`

Items with no filters always appear.

### Template variables

Each entry in `templateVars` has:

- `label` — pod label key to look up
- `urlAppend` — string appended to the URL; `$LABEL_VALUE` is replaced with the label's value

If the label doesn't exist on the pod, that `urlAppend` is skipped.

### Example

```json
{
  "menuItems": [
    {
      "description": "Datadog dashboard filtered by app label",
      "title": "Datadog App Dashboard",
      "url": "https://app.datadoghq.com/dashboard/abc-123",
      "filters": {
        "mustHaveLabels": [
          { "key": "app" }
        ]
      },
      "templateVars": [
        { "label": "app", "urlAppend": "?tpl_var_app=$LABEL_VALUE" },
        { "label": "env", "urlAppend": "&tpl_var_env=$LABEL_VALUE" }
      ]
    },
    {
      "description": "Always shows (no filters)",
      "title": "Google",
      "url": "https://google.com"
    }
  ]
}
```

For a pod with `app=nginx` and `env=production`, selecting "Datadog App Dashboard" opens:

```
https://app.datadoghq.com/dashboard/abc-123?tpl_var_app=nginx&tpl_var_env=production
```
