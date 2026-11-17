package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const valid = `
destination:
  registry: harbor.example.com
rules:
  - name: team-apps
    filters:
      name: "my-org/apps/**"
      tag: "{,v}[0-9]*.[0-9]*.[0-9]*"
    destNamespace: apps
    destNamespaceReplace: 2
  - name: charts
    override: false
    filters:
      name: "my-org/charts/**"
      tag: "*-rc*"
      tagDecoration: excludes
    destNamespace: platform/charts
`

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source.Registry != "ghcr.io" || cfg.Source.APIURL != "https://api.github.com" {
		t.Errorf("source defaults not applied: %+v", cfg.Source)
	}
	if cfg.Concurrency != DefaultConcurrency {
		t.Errorf("concurrency = %d", cfg.Concurrency)
	}
	apps, charts := cfg.Rules[0], cfg.Rules[1]
	if !apps.ShouldOverride() || apps.FlattenLevel() != 2 || apps.Filters.TagDecoration != DecorationMatches {
		t.Errorf("apps rule defaults wrong: %+v", apps)
	}
	if charts.ShouldOverride() || charts.FlattenLevel() != DefaultDestNamespaceReplace || charts.Filters.TagDecoration != DecorationExcludes {
		t.Errorf("charts rule wrong: %+v", charts)
	}
}

func TestLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, yaml string
		wantErrs   []string
	}{
		{"unknown field", "destination: {registry: h}\nrules: [{name: a, filters: {name: o/x}}]\nbogus: 1\n", []string{"bogus"}},
		{"no rules", "destination: {registry: h}\n", []string{"at least one rule"}},
		{"no destination", "rules: [{name: a, filters: {name: o/x}}]\n", []string{"destination.registry"}},
		{"url not host", "destination: {registry: 'https://h'}\nrules: [{name: a, filters: {name: o/x}}]\n", []string{"not URLs"}},
		{
			"many problems reported together",
			`destination: {registry: h}
concurrency: 100
rules:
  - name: a
    filters: {name: "*/x", tagDecoration: nope}
    destNamespaceReplace: -2
  - name: a
    filters: {name: ""}
  - filters: {name: o/x}
`,
			[]string{"concurrency", "is not valid", "tagDecoration", "destNamespaceReplace", "duplicate rule name", "filters.name is required", "rules[2]: name is required"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected error")
			}
			for _, want := range tt.wantErrs {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestExampleConfigIsValid(t *testing.T) {
	if _, err := Load("../../config.example.yaml"); err != nil {
		t.Error(err)
	}
}

// The chart writes .Values.config to config.yaml without changes. Thus the CI
// values must also pass the replicator validation.
func TestChartCIValuesConfigIsValid(t *testing.T) {
	data, err := os.ReadFile("../../charts/ghcr-replicator/ci/test-values.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var values struct {
		Config yaml.Node `yaml:"config"`
	}
	if err := yaml.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	cfg, err := yaml.Marshal(&values.Config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(cfg); err != nil {
		t.Fatal(err)
	}
}
