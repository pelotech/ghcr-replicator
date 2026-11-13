package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pelotech/ghcr-replicator/internal/config"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestCredentialsFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"none", nil, ""},
		{"all", map[string]string{"GHCR_USERNAME": "u", "GHCR_TOKEN": "t", "HARBOR_USERNAME": "robot$x", "HARBOR_PASSWORD": "p"}, ""},
		{"ghcr half", map[string]string{"GHCR_TOKEN": "t"}, "GHCR_USERNAME"},
		{"harbor half", map[string]string{"HARBOR_USERNAME": "u"}, "HARBOR_PASSWORD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := credentialsFromEnv(envFrom(tt.env))
			if tt.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSelectRules(t *testing.T) {
	configRules := []config.Rule{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	tests := []struct {
		name      string
		flagNames string
		want      []string
		wantErr   bool
	}{
		{name: "no flag runs all config rules", flagNames: "", want: []string{"a", "b", "c"}},
		{name: "flag overrides config", flagNames: "b", want: []string{"b"}},
		{name: "flag order is kept", flagNames: "c,a", want: []string{"c", "a"}},
		{name: "unknown name is an error", flagNames: "a,nope", wantErr: true},
		{name: "space is part of the name", flagNames: "a, b", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectRules(configRules, tt.flagNames)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, r := range got {
				names = append(names, r.Name)
			}
			if !slices.Equal(names, tt.want) {
				t.Errorf("got %v, want %v", names, tt.want)
			}
		})
	}
}

func TestRunExitCodes(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("destination: {registry: h}\nrules: [{name: a, filters: {name: o/x}}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want int
	}{
		{"version", []string{"-version"}, nil, 0},
		{"bad flag", []string{"-nope"}, nil, 2},
		{"bad log level", []string{"-log-level", "loud", "-config", cfgPath}, nil, 2},
		{"missing config", []string{"-config", "/does/not/exist"}, nil, 2},
		{"unknown rule", []string{"-config", cfgPath, "-rules", "zzz"}, nil, 2},
		{"half creds", []string{"-config", cfgPath}, map[string]string{"GHCR_TOKEN": "t"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.args, envFrom(tt.env)); got != tt.want {
				t.Errorf("exit = %d, want %d", got, tt.want)
			}
		})
	}
}
