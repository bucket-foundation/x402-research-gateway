// Command registry inspects, generates from, and verifies the research
// source registry.
//
//	registry validate                 # parse + validate + reconcile routes
//	registry research-index           # regenerate RESEARCH-INDEX.md
//	registry research-index -check    # fail if the generated doc is stale
//	registry verify [-only id,id]     # cheap liveness/drift check
//	registry coverage                 # lifecycle summary
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/registry"
)

const (
	defaultRegistry = "config/providers.yaml"
	defaultRoutes   = "config/routes.yaml"
	defaultProse    = "docs/research-index"
	defaultIndex    = "RESEARCH-INDEX.md"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "research-index":
		err = cmdResearchIndex(os.Args[2:])
	case "verify":
		err = cmdVerify(os.Args[2:])
	case "coverage":
		err = cmdCoverage(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "registry: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: registry <command> [flags]

commands:
  validate         parse and validate the registry, reconcile against routes
  research-index   regenerate RESEARCH-INDEX.md from the registry
  verify           cheap liveness and drift check, records last_verified
  coverage         print the lifecycle summary
`)
}

func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	regPath := fs.String("registry", defaultRegistry, "registry file")
	routesPath := fs.String("routes", defaultRoutes, "route config to reconcile against")
	_ = fs.Parse(args)

	r, err := registry.Load(*regPath)
	if err != nil {
		return err
	}

	ids, err := registry.RouteIDsFromConfig(*routesPath)
	if err != nil {
		return err
	}
	if errs := r.ReconcileRoutes(ids); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  reconcile: %v\n", e)
		}
		return fmt.Errorf("%d reconciliation problem(s) between the registry and %s", len(errs), *routesPath)
	}

	fmt.Printf("registry OK: %d providers, %d routes reconciled\n", r.Len(), len(ids))
	return nil
}

func cmdResearchIndex(args []string) error {
	fs := flag.NewFlagSet("research-index", flag.ExitOnError)
	regPath := fs.String("registry", defaultRegistry, "registry file")
	prose := fs.String("prose", defaultProse, "directory of hand-written prose partials")
	out := fs.String("out", defaultIndex, "generated document")
	check := fs.Bool("check", false, "fail if the generated document is out of date")
	_ = fs.Parse(args)

	r, err := registry.Load(*regPath)
	if err != nil {
		return err
	}
	doc, err := r.RenderIndex(*prose)
	if err != nil {
		return err
	}

	if *check {
		existing, err := os.ReadFile(*out)
		if err != nil {
			return fmt.Errorf("read %s: %w", *out, err)
		}
		if string(existing) != doc {
			return fmt.Errorf("%s is out of date; run `make research-index`", *out)
		}
		fmt.Printf("%s is up to date\n", *out)
		return nil
	}

	if err := os.WriteFile(*out, []byte(doc), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", *out, err)
	}
	fmt.Printf("wrote %s from %d providers\n", *out, r.Len())
	return nil
}

func cmdCoverage(args []string) error {
	fs := flag.NewFlagSet("coverage", flag.ExitOnError)
	regPath := fs.String("registry", defaultRegistry, "registry file")
	_ = fs.Parse(args)

	r, err := registry.Load(*regPath)
	if err != nil {
		return err
	}
	fmt.Print(r.RenderCoverage())
	return nil
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	regPath := fs.String("registry", defaultRegistry, "registry file")
	only := fs.String("only", "", "comma-separated provider ids to check")
	write := fs.Bool("write", false, "write last_verified back to the registry")
	delay := fs.Duration("delay", time.Second, "pause between providers")
	_ = fs.Parse(args)

	r, err := registry.Load(*regPath)
	if err != nil {
		return err
	}

	var ids []string
	if strings.TrimSpace(*only) != "" {
		ids = strings.Split(*only, ",")
	}

	// Ctrl-C stops the sweep without corrupting anything, since nothing is
	// written until the end.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	v := registry.NewVerifier()
	v.Delay = *delay

	results, err := v.Verify(ctx, r, ids)
	if err != nil && ctx.Err() == nil {
		return err
	}

	var stale int
	for _, res := range results {
		if res.OK {
			fmt.Printf("ok    %s\n", res.ProviderID)
			continue
		}
		stale++
		p, _ := r.Get(res.ProviderID)
		fmt.Printf("STALE %s: %s\n", res.ProviderID, p.StaleReason)
	}

	fmt.Printf("\nchecked %d provider(s), %d flagged stale\n", len(results), stale)
	if *write {
		// Verification flags entries; it never deletes them. Writing back
		// records last_verified and any stale flags.
		if err := registry.Save(*regPath, &r.File); err != nil {
			return err
		}
		fmt.Printf("recorded last_verified in %s\n", *regPath)
	}

	// A stale upstream is information, not a build failure. The exit code
	// stays zero so a flaky upstream cannot break CI; `validate` is the gate.
	return nil
}
