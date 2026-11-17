// Package config loads and validates the replicator configuration.
//
// A rule has the same settings as a Harbor pull-replication policy: name
// filter, tag filter, destination namespace, flattening level and override.
// Thus the rules can move to the Harbor GHCR adapter when it supports
// wildcards.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/pelotech/ghcr-replicator/internal/harbor/pkg/reg/adapter/githubcr"
)

const (
	DecorationMatches  = "matches"
	DecorationExcludes = "excludes"

	DefaultSourceRegistry = "ghcr.io"
	DefaultGitHubAPIURL   = "https://api.github.com"
	DefaultConcurrency    = 4
	// DefaultDestNamespaceReplace is the Harbor UI default ("Flatten 1 Level").
	DefaultDestNamespaceReplace = 1
)

type Config struct {
	Source      Source      `yaml:"source"`
	Destination Destination `yaml:"destination"`
	// Concurrency is the maximum number of repositories that one rule lists or
	// copies at the same time. The tags of one repository copy in sequence.
	Concurrency int    `yaml:"concurrency"`
	Rules       []Rule `yaml:"rules"`
}

type Source struct {
	// Registry is the host of the source registry. The default is ghcr.io.
	Registry string `yaml:"registry"`
	// APIURL is the base URL of the GitHub REST API. It is used to list packages.
	// Default: https://api.github.com.
	APIURL   string `yaml:"apiURL"`
	Insecure bool   `yaml:"insecure"`
}

type Destination struct {
	// Registry is the Harbor host, for example harbor.example.com.
	Registry string `yaml:"registry"`
	Insecure bool   `yaml:"insecure"`
}

// Rule is one replication policy. The field names are the same as in the
// Harbor replication policy model.
type Rule struct {
	// Name identifies the rule in logs and in the -rules flag. It must be unique.
	Name    string  `yaml:"name"`
	Filters Filters `yaml:"filters"`
	// DestNamespace is the destination Harbor project. It can contain "/".
	// If it is empty, the destination path is the same as the source path.
	DestNamespace string `yaml:"destNamespace"`
	// DestNamespaceReplace is the Harbor flattening level. -1 removes all
	// levels. 0 removes no levels. N removes the first N path components.
	// The default is 1.
	DestNamespaceReplace *int `yaml:"destNamespaceReplace"`
	// Override sets if the replicator replaces a destination tag that has a
	// different digest. The default is true.
	Override *bool `yaml:"override"`
}

type Filters struct {
	// Name is a doublestar pattern for the full source repository path, for
	// example "my-org/apps/**". The first path component is the owner. It
	// must be a literal name or a {a,b} list.
	Name string `yaml:"name"`
	// Tag is a doublestar pattern for tags. If it is empty, all tags match.
	Tag string `yaml:"tag"`
	// TagDecoration is "matches" or "excludes". The default is "matches".
	TagDecoration string `yaml:"tagDecoration"`
}

func (r Rule) ShouldOverride() bool { return r.Override == nil || *r.Override }
func (r Rule) FlattenLevel() int {
	if r.DestNamespaceReplace == nil {
		return DefaultDestNamespaceReplace
	}
	return *r.DestNamespaceReplace
}

// Load reads a config file, applies the defaults and validates the result.
// An unknown field causes an error.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(data)
}

// Parse decodes config YAML, applies the defaults and validates the result.
func Parse(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Source.Registry == "" {
		c.Source.Registry = DefaultSourceRegistry
	}
	if c.Source.APIURL == "" {
		c.Source.APIURL = DefaultGitHubAPIURL
	}
	if c.Concurrency == 0 {
		c.Concurrency = DefaultConcurrency
	}
	for i := range c.Rules {
		if c.Rules[i].Filters.TagDecoration == "" {
			c.Rules[i].Filters.TagDecoration = DecorationMatches
		}
	}
}

// Validate returns all problems that it finds, not only the first problem.
func (c *Config) Validate() error {
	var errs []error
	if c.Destination.Registry == "" {
		errs = append(errs, errors.New("destination.registry is required"))
	}
	if strings.Contains(c.Destination.Registry, "://") || strings.Contains(c.Source.Registry, "://") {
		errs = append(errs, errors.New("registries are hosts (e.g. harbor.example.com), not URLs; use insecure: true for plain HTTP"))
	}
	if c.Concurrency < 1 || c.Concurrency > 64 {
		errs = append(errs, fmt.Errorf("concurrency must be between 1 and 64, got %d", c.Concurrency))
	}
	if len(c.Rules) == 0 {
		errs = append(errs, errors.New("at least one rule is required"))
	}
	seen := map[string]bool{}
	for i, r := range c.Rules {
		where := fmt.Sprintf("rules[%d]", i)
		if r.Name == "" {
			errs = append(errs, fmt.Errorf("%s: name is required", where))
		} else {
			where = fmt.Sprintf("rule %q", r.Name)
			if seen[r.Name] {
				errs = append(errs, fmt.Errorf("%s: duplicate rule name", where))
			}
			seen[r.Name] = true
		}
		if r.Filters.Name == "" {
			errs = append(errs, fmt.Errorf("%s: filters.name is required", where))
		} else if _, err := githubcr.Owners(r.Filters.Name); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", where, err))
		}
		switch r.Filters.TagDecoration {
		case DecorationMatches, DecorationExcludes:
		default:
			errs = append(errs, fmt.Errorf("%s: filters.tagDecoration must be %q or %q, got %q",
				where, DecorationMatches, DecorationExcludes, r.Filters.TagDecoration))
		}
		if r.FlattenLevel() < -1 {
			errs = append(errs, fmt.Errorf("%s: destNamespaceReplace must be >= -1, got %d", where, r.FlattenLevel()))
		}
	}
	return errors.Join(errs...)
}
