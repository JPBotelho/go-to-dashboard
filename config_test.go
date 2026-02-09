package main

import (
	"encoding/json"
	"testing"
)

// podFromJSON is a test helper that creates a PodData from a raw JSON string.
func podFromJSON(t *testing.T, raw string) *PodData {
	t.Helper()
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("podFromJSON: %v", err)
	}
	return &PodData{
		Name:      "test-pod",
		Namespace: "default",
		RawJSON:   []byte(raw),
		Parsed:    parsed,
	}
}

// mustCompileCondition validates a single-item config to compile condition regexes.
func mustCompileCondition(t *testing.T, c Condition) Condition {
	t.Helper()
	cfg := Config{MenuItems: []MenuItem{{
		Title:   "test",
		URL:     "http://test",
		Filters: ItemFilters{Conditions: []Condition{c}},
	}}}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatalf("mustCompileCondition: %v", err)
	}
	return cfg.MenuItems[0].Filters.Conditions[0]
}

// --- Example pod JSON strings ---

const podNginxProd = `{
  "apiVersion": "v1",
  "kind": "Pod",
  "metadata": {
    "name": "nginx-abc123",
    "namespace": "production",
    "labels": {
      "app": "nginx",
      "env": "production",
      "team": "platform"
    },
    "annotations": {
      "prometheus.io/scrape": "true",
      "prometheus.io/port": "9090"
    }
  },
  "spec": {
    "nodeName": "prod-pool-node-01",
    "containers": [
      {
        "name": "nginx",
        "image": "nginx:1.25"
      },
      {
        "name": "sidecar",
        "image": "envoy:1.28"
      }
    ],
    "restartPolicy": "Always"
  },
  "status": {
    "phase": "Running",
    "conditions": [
      {"type": "Ready", "status": "True"}
    ]
  }
}`

const podRedisStaging = `{
  "apiVersion": "v1",
  "kind": "Pod",
  "metadata": {
    "name": "redis-xyz789",
    "namespace": "staging",
    "labels": {
      "app": "redis",
      "env": "staging"
    }
  },
  "spec": {
    "nodeName": "staging-node-03",
    "containers": [
      {
        "name": "redis",
        "image": "redis:7"
      }
    ]
  },
  "status": {
    "phase": "Running"
  }
}`

const podNoLabels = `{
  "apiVersion": "v1",
  "kind": "Pod",
  "metadata": {
    "name": "bare-pod",
    "namespace": "default"
  },
  "spec": {
    "nodeName": "default-node-01"
  },
  "status": {
    "phase": "Pending"
  }
}`

// ---- ResolvePath tests ----

func TestResolvePath(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)

	tests := []struct {
		name   string
		path   string
		wantOK bool
		check  func(t *testing.T, val interface{})
	}{
		{
			name: "scalar string",
			path: "metadata.name", wantOK: true,
			check: func(t *testing.T, val interface{}) {
				if val != "nginx-abc123" {
					t.Errorf("got %v, want nginx-abc123", val)
				}
			},
		},
		{
			name: "map (labels)",
			path: "metadata.labels", wantOK: true,
			check: func(t *testing.T, val interface{}) {
				m, ok := val.(map[string]interface{})
				if !ok {
					t.Fatalf("expected map, got %T", val)
				}
				if m["app"] != "nginx" {
					t.Errorf("labels.app = %v, want nginx", m["app"])
				}
			},
		},
		{
			name: "nested scalar",
			path: "status.phase", wantOK: true,
			check: func(t *testing.T, val interface{}) {
				if val != "Running" {
					t.Errorf("got %v, want Running", val)
				}
			},
		},
		{
			name: "array (containers)",
			path: "spec.containers", wantOK: true,
			check: func(t *testing.T, val interface{}) {
				arr, ok := val.([]interface{})
				if !ok {
					t.Fatalf("expected array, got %T", val)
				}
				if len(arr) != 2 {
					t.Errorf("got %d containers, want 2", len(arr))
				}
			},
		},
		{
			name:   "missing path",
			path:   "metadata.nonexistent",
			wantOK: false,
		},
		{
			name:   "deeply missing path",
			path:   "metadata.labels.app.deep",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := pd.ResolvePath(tt.path)
			if ok != tt.wantOK {
				t.Fatalf("ResolvePath(%q) ok = %v, want %v", tt.path, ok, tt.wantOK)
			}
			if ok && tt.check != nil {
				tt.check(t, val)
			}
		})
	}
}

// ---- Condition.Evaluate tests ----

func TestConditionEvaluate_MapLabels(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)

	tests := []struct {
		name string
		cond Condition
		want bool
	}{
		{
			name: "exact key exists (any value)",
			cond: Condition{Path: "metadata.labels", KeyPattern: "app"},
			want: true,
		},
		{
			name: "exact key+value match",
			cond: Condition{Path: "metadata.labels", KeyPattern: "app", ValuePattern: "nginx"},
			want: true,
		},
		{
			name: "exact key, wrong value",
			cond: Condition{Path: "metadata.labels", KeyPattern: "app", ValuePattern: "redis"},
			want: false,
		},
		{
			name: "key does not exist",
			cond: Condition{Path: "metadata.labels", KeyPattern: "version"},
			want: false,
		},
		{
			name: "key regex wildcard matches any key",
			cond: Condition{Path: "metadata.labels", KeyPattern: ".*"},
			want: true,
		},
		{
			name: "key regex prefix match",
			cond: Condition{Path: "metadata.labels", KeyPattern: "te.*"},
			want: true, // matches "team"
		},
		{
			name: "key regex substring with .*",
			cond: Condition{Path: "metadata.labels", KeyPattern: ".*ea.*"},
			want: true, // matches "team"
		},
		{
			name: "value regex prefix match",
			cond: Condition{Path: "metadata.labels", KeyPattern: "app", ValuePattern: "ngi.*"},
			want: true,
		},
		{
			name: "implicit anchoring prevents substring match",
			cond: Condition{Path: "metadata.labels", KeyPattern: "ap"},
			want: false, // "ap" anchored to ^ap$ does NOT match "app"
		},
		{
			name: "implicit anchoring on value prevents substring",
			cond: Condition{Path: "metadata.labels", KeyPattern: "app", ValuePattern: "ngi"},
			want: false, // "ngi" anchored to ^ngi$ does NOT match "nginx"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mustCompileCondition(t, tt.cond)
			got := c.Evaluate(pd)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConditionEvaluate_MapAnnotations(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)

	tests := []struct {
		name string
		cond Condition
		want bool
	}{
		{
			name: "annotation key exists",
			cond: Condition{Path: "metadata.annotations", KeyPattern: "prometheus\\.io/scrape"},
			want: true,
		},
		{
			name: "annotation key+value",
			cond: Condition{Path: "metadata.annotations", KeyPattern: "prometheus\\.io/port", ValuePattern: "9090"},
			want: true,
		},
		{
			name: "annotation key regex matches multiple",
			cond: Condition{Path: "metadata.annotations", KeyPattern: "prometheus\\.io/.*"},
			want: true,
		},
		{
			name: "annotation missing key",
			cond: Condition{Path: "metadata.annotations", KeyPattern: "vault\\.hashicorp\\.com/inject"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mustCompileCondition(t, tt.cond)
			if got := c.Evaluate(pd); got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConditionEvaluate_Scalar(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)

	tests := []struct {
		name string
		cond Condition
		want bool
	}{
		{
			name: "exact scalar match",
			cond: Condition{Path: "status.phase", ValuePattern: "Running"},
			want: true,
		},
		{
			name: "scalar no match",
			cond: Condition{Path: "status.phase", ValuePattern: "Pending"},
			want: false,
		},
		{
			name: "scalar regex",
			cond: Condition{Path: "spec.nodeName", ValuePattern: "prod-pool-.*"},
			want: true,
		},
		{
			name: "scalar regex no match",
			cond: Condition{Path: "spec.nodeName", ValuePattern: "staging-.*"},
			want: false,
		},
		{
			name: "scalar restartPolicy",
			cond: Condition{Path: "spec.restartPolicy", ValuePattern: "Always"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mustCompileCondition(t, tt.cond)
			if got := c.Evaluate(pd); got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConditionEvaluate_Invert(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)

	tests := []struct {
		name string
		cond Condition
		want bool
	}{
		{
			name: "invert existing label → false",
			cond: Condition{Path: "metadata.labels", KeyPattern: "app", Invert: true},
			want: false,
		},
		{
			name: "invert missing label → true",
			cond: Condition{Path: "metadata.labels", KeyPattern: "version", Invert: true},
			want: true,
		},
		{
			name: "invert missing path → true",
			cond: Condition{Path: "metadata.nonexistent", Invert: true},
			want: true,
		},
		{
			name: "invert scalar match → false",
			cond: Condition{Path: "status.phase", ValuePattern: "Running", Invert: true},
			want: false,
		},
		{
			name: "invert scalar mismatch → true",
			cond: Condition{Path: "status.phase", ValuePattern: "Failed", Invert: true},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mustCompileCondition(t, tt.cond)
			if got := c.Evaluate(pd); got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConditionEvaluate_MissingPath(t *testing.T) {
	pd := podFromJSON(t, podNoLabels)

	// Pod has no labels at all
	c := mustCompileCondition(t, Condition{Path: "metadata.labels", KeyPattern: "app"})
	if got := c.Evaluate(pd); got != false {
		t.Errorf("missing labels map: Evaluate() = %v, want false", got)
	}

	// Inverted: missing labels → true
	c2 := mustCompileCondition(t, Condition{Path: "metadata.labels", KeyPattern: "app", Invert: true})
	if got := c2.Evaluate(pd); got != true {
		t.Errorf("missing labels map (inverted): Evaluate() = %v, want true", got)
	}
}

// ---- MatchesPod (AND logic) tests ----

func TestMatchesPod(t *testing.T) {
	nginx := podFromJSON(t, podNginxProd)
	redis := podFromJSON(t, podRedisStaging)

	// Build a menu item requiring app=nginx AND env=production
	item := MenuItem{
		Title: "test", URL: "http://test",
		Filters: ItemFilters{Conditions: []Condition{
			{Path: "metadata.labels", KeyPattern: "app", ValuePattern: "nginx"},
			{Path: "metadata.labels", KeyPattern: "env", ValuePattern: "production"},
		}},
	}
	cfg := Config{MenuItems: []MenuItem{item}}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	item = cfg.MenuItems[0]

	if !item.MatchesPod(nginx) {
		t.Error("nginx-prod pod should match app=nginx AND env=production")
	}
	if item.MatchesPod(redis) {
		t.Error("redis-staging pod should NOT match app=nginx AND env=production")
	}
}

func TestMatchesPod_NoConditions(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)
	item := MenuItem{Title: "test", URL: "http://test"}
	// No conditions → always matches
	if !item.MatchesPod(pd) {
		t.Error("item with no conditions should match any pod")
	}
}

func TestMatchesPod_MixedPathTypes(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)

	// Require label app=nginx AND status.phase=Running AND nodeName starts with prod-
	item := MenuItem{
		Title: "test", URL: "http://test",
		Filters: ItemFilters{Conditions: []Condition{
			{Path: "metadata.labels", KeyPattern: "app", ValuePattern: "nginx"},
			{Path: "status.phase", ValuePattern: "Running"},
			{Path: "spec.nodeName", ValuePattern: "prod-.*"},
		}},
	}
	cfg := Config{MenuItems: []MenuItem{item}}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	if !cfg.MenuItems[0].MatchesPod(pd) {
		t.Error("nginx-prod should match all three conditions")
	}

	// Same conditions against redis-staging → should fail
	redis := podFromJSON(t, podRedisStaging)
	if cfg.MenuItems[0].MatchesPod(redis) {
		t.Error("redis-staging should NOT match (app!=nginx, node!=prod-*)")
	}
}

// ---- FilterMenuItems tests ----

func TestFilterMenuItems_NilPod(t *testing.T) {
	items := []MenuItem{
		{Title: "With filter", URL: "http://a", Filters: ItemFilters{Conditions: []Condition{
			{Path: "metadata.labels", KeyPattern: "app"},
		}}},
		{Title: "No filter", URL: "http://b"},
	}
	cfg := Config{MenuItems: items}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	result := FilterMenuItems(cfg.MenuItems, nil)
	if len(result) != 1 || result[0].Title != "No filter" {
		t.Errorf("nil pod: got %d items, want 1 (No filter)", len(result))
	}
}

func TestFilterMenuItems_WithPod(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)

	items := []MenuItem{
		{Title: "Matches", URL: "http://a", Filters: ItemFilters{Conditions: []Condition{
			{Path: "metadata.labels", KeyPattern: "app", ValuePattern: "nginx"},
		}}},
		{Title: "No match", URL: "http://b", Filters: ItemFilters{Conditions: []Condition{
			{Path: "metadata.labels", KeyPattern: "app", ValuePattern: "redis"},
		}}},
		{Title: "Always", URL: "http://c"},
	}
	cfg := Config{MenuItems: items}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	result := FilterMenuItems(cfg.MenuItems, pd)
	if len(result) != 2 {
		t.Fatalf("got %d items, want 2", len(result))
	}
	if result[0].Title != "Matches" || result[1].Title != "Always" {
		t.Errorf("got [%s, %s], want [Matches, Always]", result[0].Title, result[1].Title)
	}
}

// ---- ResolveURL tests ----

func TestResolveURL_LabelPath(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)

	item := MenuItem{
		Title: "test", URL: "https://example.com/dashboard",
		TemplateVars: []TemplateVar{
			{Path: "metadata.labels.app", URLAppend: "?app=$VALUE"},
		},
	}
	cfg := Config{MenuItems: []MenuItem{item}}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	got := cfg.MenuItems[0].ResolveURL(pd)
	want := "https://example.com/dashboard?app=nginx"
	if got != want {
		t.Errorf("ResolveURL = %q, want %q", got, want)
	}
}

func TestResolveURL_ScalarPath(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)

	item := MenuItem{
		Title: "test", URL: "https://example.com/nodes",
		TemplateVars: []TemplateVar{
			{Path: "spec.nodeName", URLAppend: "?node=$VALUE"},
		},
	}
	cfg := Config{MenuItems: []MenuItem{item}}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	got := cfg.MenuItems[0].ResolveURL(pd)
	want := "https://example.com/nodes?node=prod-pool-node-01"
	if got != want {
		t.Errorf("ResolveURL = %q, want %q", got, want)
	}
}

func TestResolveURL_MultipleReplacements(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)

	item := MenuItem{
		Title: "test", URL: "https://example.com",
		TemplateVars: []TemplateVar{
			{Path: "metadata.labels.app", URLAppend: "?app=$VALUE&extra=$VALUE"},
		},
	}
	cfg := Config{MenuItems: []MenuItem{item}}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	got := cfg.MenuItems[0].ResolveURL(pd)
	want := "https://example.com?app=nginx&extra=nginx"
	if got != want {
		t.Errorf("ResolveURL = %q, want %q", got, want)
	}
}

func TestResolveURL_MissingKey(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)

	item := MenuItem{
		Title: "test", URL: "https://example.com",
		TemplateVars: []TemplateVar{
			{Path: "metadata.labels.nonexistent", URLAppend: "?x=$VALUE"},
		},
	}
	cfg := Config{MenuItems: []MenuItem{item}}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	got := cfg.MenuItems[0].ResolveURL(pd)
	want := "https://example.com" // nothing appended
	if got != want {
		t.Errorf("ResolveURL = %q, want %q", got, want)
	}
}

func TestResolveURL_MultipleVars(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)

	item := MenuItem{
		Title: "test", URL: "https://example.com",
		TemplateVars: []TemplateVar{
			{Path: "metadata.labels.app", URLAppend: "?app=$VALUE"},
			{Path: "metadata.name", URLAppend: "&pod=$VALUE"},
		},
	}
	cfg := Config{MenuItems: []MenuItem{item}}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	got := cfg.MenuItems[0].ResolveURL(pd)
	want := "https://example.com?app=nginx&pod=nginx-abc123"
	if got != want {
		t.Errorf("ResolveURL = %q, want %q", got, want)
	}
}

func TestResolveURL_NilPod(t *testing.T) {
	item := MenuItem{
		Title: "test", URL: "https://example.com",
		TemplateVars: []TemplateVar{
			{Path: "metadata.labels.app", URLAppend: "?app=$VALUE"},
		},
	}
	cfg := Config{MenuItems: []MenuItem{item}}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	got := cfg.MenuItems[0].ResolveURL(nil)
	want := "https://example.com"
	if got != want {
		t.Errorf("ResolveURL(nil) = %q, want %q", got, want)
	}
}

// ---- Labels() convenience ----

func TestLabelsConvenience(t *testing.T) {
	pd := podFromJSON(t, podNginxProd)
	labels := pd.Labels()
	if labels["app"] != "nginx" {
		t.Errorf("Labels()[app] = %q, want nginx", labels["app"])
	}
	if labels["env"] != "production" {
		t.Errorf("Labels()[env] = %q, want production", labels["env"])
	}

	// Pod without labels
	bare := podFromJSON(t, podNoLabels)
	if len(bare.Labels()) != 0 {
		t.Errorf("bare pod Labels() should be empty, got %v", bare.Labels())
	}
}

// ---- ValidateConfig tests ----

func TestValidateConfig_InvalidRegex(t *testing.T) {
	cfg := Config{MenuItems: []MenuItem{{
		Title: "test", URL: "http://test",
		Filters: ItemFilters{Conditions: []Condition{
			{Path: "metadata.labels", KeyPattern: "[invalid"},
		}},
	}}}
	if err := ValidateConfig(&cfg); err == nil {
		t.Error("expected error for invalid regex, got nil")
	}
}

func TestValidateConfig_DefaultPatterns(t *testing.T) {
	cfg := Config{MenuItems: []MenuItem{{
		Title: "test", URL: "http://test",
		Filters: ItemFilters{Conditions: []Condition{
			{Path: "metadata.labels"},
		}},
	}}}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	cond := cfg.MenuItems[0].Filters.Conditions[0]
	if cond.KeyPattern != ".*" {
		t.Errorf("default KeyPattern = %q, want .*", cond.KeyPattern)
	}
	if cond.ValuePattern != ".*" {
		t.Errorf("default ValuePattern = %q, want .*", cond.ValuePattern)
	}
}

// ---- anchorPattern tests ----

func TestAnchorPattern(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"app", "^app$"},
		{".*", "^.*$"},
		{"app.*", "^app.*$"},
		{"^already", "^already$"},
		{"already$", "^already$"},
		{"^both$", "^both$"},
	}
	for _, tt := range tests {
		got := anchorPattern(tt.input)
		if got != tt.want {
			t.Errorf("anchorPattern(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---- End-to-end scenario ----

func TestEndToEnd_MultiPodFilterAndResolve(t *testing.T) {
	// Simulate a config with varied items
	cfg := Config{MenuItems: []MenuItem{
		{
			Title: "Nginx Prod Dashboard", URL: "https://grafana.example.com/nginx",
			Filters: ItemFilters{Conditions: []Condition{
				{Path: "metadata.labels", KeyPattern: "app", ValuePattern: "nginx"},
				{Path: "metadata.labels", KeyPattern: "env", ValuePattern: "production"},
			}},
		},
		{
			Title: "Any App Dashboard", URL: "https://grafana.example.com/apps",
			Filters: ItemFilters{Conditions: []Condition{
				{Path: "metadata.labels", KeyPattern: "app"},
			}},
			TemplateVars: []TemplateVar{
				{Path: "metadata.labels.app", URLAppend: "?app=$VALUE"},
			},
		},
		{
			Title: "Running Pods Only", URL: "https://example.com/running",
			Filters: ItemFilters{Conditions: []Condition{
				{Path: "status.phase", ValuePattern: "Running"},
			}},
		},
		{
			Title: "Prod Nodes", URL: "https://example.com/nodes",
			Filters: ItemFilters{Conditions: []Condition{
				{Path: "spec.nodeName", ValuePattern: "prod-.*"},
			}},
			TemplateVars: []TemplateVar{
				{Path: "spec.nodeName", URLAppend: "?node=$VALUE"},
			},
		},
		{
			Title: "Always Visible", URL: "https://google.com",
		},
	}}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	// --- nginx-prod pod ---
	nginx := podFromJSON(t, podNginxProd)
	nginxItems := FilterMenuItems(cfg.MenuItems, nginx)
	nginxTitles := make([]string, len(nginxItems))
	for i, it := range nginxItems {
		nginxTitles[i] = it.Title
	}
	wantNginx := []string{"Nginx Prod Dashboard", "Any App Dashboard", "Running Pods Only", "Prod Nodes", "Always Visible"}
	if len(nginxTitles) != len(wantNginx) {
		t.Fatalf("nginx: got titles %v, want %v", nginxTitles, wantNginx)
	}
	for i := range wantNginx {
		if nginxTitles[i] != wantNginx[i] {
			t.Errorf("nginx[%d] = %q, want %q", i, nginxTitles[i], wantNginx[i])
		}
	}

	// Check URL resolution for "Any App Dashboard"
	appURL := nginxItems[1].ResolveURL(nginx)
	if appURL != "https://grafana.example.com/apps?app=nginx" {
		t.Errorf("app URL = %q", appURL)
	}

	// Check URL resolution for "Prod Nodes"
	nodeURL := nginxItems[3].ResolveURL(nginx)
	if nodeURL != "https://example.com/nodes?node=prod-pool-node-01" {
		t.Errorf("node URL = %q", nodeURL)
	}

	// --- redis-staging pod ---
	redis := podFromJSON(t, podRedisStaging)
	redisItems := FilterMenuItems(cfg.MenuItems, redis)
	redisTitles := make([]string, len(redisItems))
	for i, it := range redisItems {
		redisTitles[i] = it.Title
	}
	// redis-staging: no nginx label, not prod node → should get: "Any App Dashboard", "Running Pods Only", "Always Visible"
	wantRedis := []string{"Any App Dashboard", "Running Pods Only", "Always Visible"}
	if len(redisTitles) != len(wantRedis) {
		t.Fatalf("redis: got titles %v, want %v", redisTitles, wantRedis)
	}
	for i := range wantRedis {
		if redisTitles[i] != wantRedis[i] {
			t.Errorf("redis[%d] = %q, want %q", i, redisTitles[i], wantRedis[i])
		}
	}

	// --- no-labels pod ---
	bare := podFromJSON(t, podNoLabels)
	bareItems := FilterMenuItems(cfg.MenuItems, bare)
	bareTitles := make([]string, len(bareItems))
	for i, it := range bareItems {
		bareTitles[i] = it.Title
	}
	// no labels, phase=Pending, node=default-node-01 → only "Always Visible"
	wantBare := []string{"Always Visible"}
	if len(bareTitles) != len(wantBare) {
		t.Fatalf("bare: got titles %v, want %v", bareTitles, wantBare)
	}

	// --- nil pod ---
	nilItems := FilterMenuItems(cfg.MenuItems, nil)
	if len(nilItems) != 1 || nilItems[0].Title != "Always Visible" {
		t.Errorf("nil pod: got %d items, want 1 (Always Visible)", len(nilItems))
	}
}
