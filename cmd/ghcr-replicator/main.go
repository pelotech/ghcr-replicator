// Command ghcr-replicator copies container images from ghcr.io to Harbor.
// It uses Harbor-style replication rules. A rule can use a wildcard name
// filter. The Harbor GHCR adapter does not support these filters.
//
// The command reads the credentials from these environment variables:
//
//	GHCR_USERNAME, GHCR_TOKEN         Pull from ghcr.io and list packages.
//	                                  The token must have read:packages.
//	HARBOR_USERNAME, HARBOR_PASSWORD  Push to Harbor. Use a robot account.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/pelotech/ghcr-replicator/internal/config"
	"github.com/pelotech/ghcr-replicator/internal/harbor/pkg/reg/adapter/githubcr"
	"github.com/pelotech/ghcr-replicator/internal/replicate"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Getenv))
}

func run(args []string, getenv func(string) string) int {
	fs := flag.NewFlagSet("ghcr-replicator", flag.ContinueOnError)
	var (
		configPath  = fs.String("config", "/etc/ghcr-replicator/config.yaml", "path to the config file")
		dryRun      = fs.Bool("dry-run", false, "plan only: list what would be copied without writing to the destination")
		ruleNames   = fs.String("rules", "", "comma-separated rule names to run (default: all rules)")
		logLevel    = fs.String("log-level", "info", "debug, info, warn or error")
		timeout     = fs.Duration("timeout", 55*time.Minute, "overall run timeout")
		showVersion = fs.Bool("version", false, "print the version and exit")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Println(version)
		return 0
	}

	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(*logLevel)); err != nil {
		fmt.Fprintf(os.Stderr, "invalid -log-level %q\n", *logLevel)
		return 2
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Error("load config", "err", err)
		return 2
	}
	rules, err := selectRules(cfg.Rules, *ruleNames)
	if err != nil {
		log.Error("select rules", "err", err)
		return 2
	}
	creds, err := credentialsFromEnv(getenv)
	if err != nil {
		log.Error("credentials", "err", err)
		return 2
	}

	kc := replicate.Keychain{}
	kc.Add(cfg.Source.Registry, creds.ghcrUser, creds.ghcrToken)
	kc.Add(cfg.Destination.Registry, creds.harborUser, creds.harborPassword)

	ua := "ghcr-replicator/" + version
	lister := githubcr.NewLister(cfg.Source.APIURL, creds.ghcrUser, creds.ghcrToken, ua)

	r, err := replicate.New(cfg, replicate.Options{
		Lister:    lister,
		Keychain:  kc,
		UserAgent: ua,
		DryRun:    *dryRun,
		Logger:    log,
	})
	if err != nil {
		log.Error("init", "err", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	log.Info("starting", "version", version, "source", cfg.Source.Registry, "destination", cfg.Destination.Registry,
		"rules", len(rules), "dryRun", *dryRun)
	results, err := r.Run(ctx, rules)

	var total replicate.Result
	for _, res := range results {
		total.Copied += res.Copied
		total.WouldCopy += res.WouldCopy
		total.UpToDate += res.UpToDate
		total.Skipped += res.Skipped
		total.Failed += res.Failed
	}
	log.Info("finished", "copied", total.Copied, "wouldCopy", total.WouldCopy, "upToDate", total.UpToDate, "skipped", total.Skipped,
		"failed", total.Failed, "dryRun", *dryRun)

	if err != nil {
		log.Error("run interrupted", "err", err)
		return 1
	}
	if total.Failed > 0 {
		return 1
	}
	return 0
}

type credentials struct {
	ghcrUser, ghcrToken        string
	harborUser, harborPassword string
}

func credentialsFromEnv(getenv func(string) string) (credentials, error) {
	c := credentials{
		ghcrUser:       getenv("GHCR_USERNAME"),
		ghcrToken:      getenv("GHCR_TOKEN"),
		harborUser:     getenv("HARBOR_USERNAME"),
		harborPassword: getenv("HARBOR_PASSWORD"),
	}
	var errs []error
	if (c.ghcrUser == "") != (c.ghcrToken == "") {
		errs = append(errs, errors.New("set both GHCR_USERNAME and GHCR_TOKEN, or neither"))
	}
	if (c.harborUser == "") != (c.harborPassword == "") {
		errs = append(errs, errors.New("set both HARBOR_USERNAME and HARBOR_PASSWORD, or neither"))
	}
	return c, errors.Join(errs...)
}

func selectRules(configRules []config.Rule, flagNames string) ([]config.Rule, error) {
	if flagNames == "" {
		return configRules, nil
	}
	var out []config.Rule
	for _, name := range strings.Split(flagNames, ",") {
		i := slices.IndexFunc(configRules, func(r config.Rule) bool { return r.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("unknown rule %q", name)
		}
		out = append(out, configRules[i])
	}
	return out, nil
}
