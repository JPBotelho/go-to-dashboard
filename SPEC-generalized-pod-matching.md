# Spec: Generalized Pod Field Matching

## 1. Goal

Replace the label-only filtering system with a generic mechanism that can match against **any field** in the pod's JSON representation, using dot-notation paths and regex patterns.

## 2. Pod Data Changes

**Current:** `FetchLabels()` unmarshals only `metadata.labels` into `map[string]string`.

**Proposed:** `FetchPodJSON()` stores the **entire** raw JSON output from `kubectl get pod -o json`. Fields are accessed at match time via dot-notation path resolution.

```go
type PodData struct {
    Name      string
    Namespace string
    RawJSON   []byte                 // full kubectl output
    Parsed    map[string]interface{} // unmarshaled for traversal
}
```

A helper function `ResolvePath(path string) (interface{}, bool)` walks the `Parsed` map using a dot-separated path like `metadata.labels` or `spec.nodeName` and returns whatever is at that location (a map, a string, a number, etc.).

**Library consideration:** [`gjson`](https://github.com/tidwall/gjson) is a single-file, zero-dependency Go library that does exactly this. `gjson.GetBytes(rawJSON, "metadata.labels")` returns the subtree. It supports dot paths, array wildcards (`spec.containers.#.image`), and more. Alternatively, a ~30-line hand-rolled walker on `map[string]interface{}` works for basic dot paths.

## 3. Config Schema Changes

### 3.1 Filters: `conditions` replaces `mustHaveLabels`

```json
{
  "filters": {
    "conditions": [
      {
        "path": "metadata.labels",
        "keyPattern": "app",
        "valuePattern": "nginx",
        "invert": false
      }
    ]
  }
}
```

**Implicit anchoring:** All patterns are automatically wrapped in `^...$` before compilation. This means `app` matches exactly `app`, not a substring like `myapp`. To opt into regex behavior, just use regex syntax: `app.*` matches `app`, `app-v2`, etc. `.*team.*` matches anything containing "team". `.*` matches everything.

| Field          | Type   | Default      | Description                                                                                                                                                                                                                     |
| -------------- | ------ | ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `path`         | string | **required** | Dot-notation path into the pod JSON (e.g. `metadata.labels`, `spec.nodeName`, `status.phase`).                                                                                                                                  |
| `keyPattern`   | string | `".*"`       | Regex applied to map keys (implicitly anchored). Only meaningful when `path` resolves to a map. Omit or set to `".*"` to match all keys.                                                                                        |
| `valuePattern` | string | `".*"`       | Regex applied to the value (implicitly anchored). For maps, applied to the values of entries whose keys matched `keyPattern`. For scalars, applied to the scalar value directly. Omit or set to `".*"` to accept any value (= "key must exist" semantics). |
| `invert`       | bool   | `false`      | If `true`, the condition passes when the match **fails**. Enables negative filtering ("must NOT have").                                                                                                                          |

### 3.2 Match Semantics by Resolved Type

| Resolved type              | Behavior                                                                                                                                                                                                                                                       |
| -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Map**                    | Condition passes if **at least one** entry has a key matching `keyPattern` AND a value matching `valuePattern`. With `invert: true`, passes if **no** entry matches.                                                                                            |
| **Scalar** (string/number/bool) | `keyPattern` is ignored. Condition passes if the stringified value matches `valuePattern`. With `invert: true`, passes if it does NOT match.                                                                                                               |
| **Array**                  | Condition passes if **at least one** element (stringified) matches `valuePattern`. `keyPattern` is ignored. With `invert: true`, passes if no element matches. For arrays of objects, consider supporting extended paths like `spec.containers.#.image` if using gjson, or deferring to a later version. |
| **Missing / null**         | Condition **fails** (the field doesn't exist). With `invert: true`, it **passes** (useful for "must NOT have field X").                                                                                                                                         |

All conditions in a filter are ANDed together (same as current `mustHaveLabels`).

### 3.3 TemplateVars: Generalized Value Extraction

```json
{
  "templateVars": [
    {
      "path": "metadata.labels",
      "key": "app",
      "placeholder": "$APP_LABEL",
      "urlAppend": "?tpl_var_app=$APP_LABEL"
    },
    {
      "path": "spec.nodeName",
      "placeholder": "$NODE",
      "urlAppend": "&node=$NODE"
    }
  ]
}
```

| Field         | Type   | Default      | Description                                                                                                    |
| ------------- | ------ | ------------ | -------------------------------------------------------------------------------------------------------------- |
| `path`        | string | **required** | Dot-notation path to resolve.                                                                                  |
| `key`         | string | optional     | For map-valued paths: the exact key whose value to extract. For scalars: omitted.                               |
| `placeholder` | string | `"$VALUE"`   | The placeholder string to replace in `urlAppend`. Defaults to `$VALUE` for backward compat.                     |
| `urlAppend`   | string | **required** | String appended to the URL. All occurrences of `placeholder` are replaced with the resolved value.              |

## 4. Examples

### 4.1 Label matching (equivalent to old `mustHaveLabels`)

```json
{
  "filters": {
    "conditions": [
      { "path": "metadata.labels", "keyPattern": "app" },
      { "path": "metadata.labels", "keyPattern": "env", "valuePattern": "production" }
    ]
  }
}
```

`"keyPattern": "app"` is implicitly anchored to `^app$`, so it matches exactly "app".

### 4.2 Negative match: exclude pods with a specific annotation

```json
{
  "filters": {
    "conditions": [
      { "path": "metadata.annotations", "keyPattern": "internal\\.skip-dashboard", "invert": true }
    ]
  }
}
```

This menu item shows for all pods that do **not** have the annotation `internal.skip-dashboard`.

### 4.3 Match on arbitrary fields

```json
{
  "filters": {
    "conditions": [
      { "path": "status.phase", "valuePattern": "Running" },
      { "path": "spec.nodeName", "valuePattern": "prod-pool-.*" }
    ]
  }
}
```

Only shows for running pods on nodes whose name starts with `prod-pool-`.

### 4.4 Match any label key containing a substring

```json
{
  "filters": {
    "conditions": [
      { "path": "metadata.labels", "keyPattern": ".*team.*" }
    ]
  }
}
```

Shows for any pod that has at least one label with "team" in its key.

### 4.5 TemplateVar using a non-label field

```json
{
  "templateVars": [
    {
      "path": "metadata.name",
      "placeholder": "$POD_NAME",
      "urlAppend": "&pod=$POD_NAME"
    },
    {
      "path": "metadata.labels",
      "key": "app",
      "placeholder": "$APP",
      "urlAppend": "?app=$APP"
    }
  ]
}
```

## 5. Implementation Notes

- **Path resolution:** If using `gjson`, `gjson.GetBytes(raw, "metadata.labels")` returns a `Result` that can be iterated as a map. For scalars, `.String()` suffices. If hand-rolling, a simple `strings.Split(path, ".")` loop walking `map[string]interface{}` covers 90% of use cases.
- **Regex compilation:** Wrap each pattern in `^...$` if not already anchored, then compile once during config validation (`regexp.Compile`). Store the compiled `*regexp.Regexp` alongside the condition. Fail fast on invalid regex at config load time.
- **Stringification for matching:** Numbers become their decimal string, bools become `"true"`/`"false"`, nulls don't match anything (treated as missing).
- **Validation changes:** `ValidateConfig` should verify that `path` is non-empty, and that `keyPattern`/`valuePattern` are valid regexes.
- **Preview pane in fzf:** Currently shows "All Pod Labels". Could be generalized to show the matched fields and their values. This is a UI concern and can be addressed separately.

## 6. Scope Boundary

**In scope:** Conditions on maps, scalars, and simple arrays. Template vars for maps (by key) and scalars.

**Deferred:** Deep array-of-objects traversal (e.g. `spec.containers[*].image`). If needed later, adopting `gjson` makes this trivial (`spec.containers.#.image` returns an array of all container images).
