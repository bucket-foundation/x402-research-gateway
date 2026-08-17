package registry

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// File is the on-disk registry document.
type File struct {
	// Version lets the format evolve without guessing at an unversioned file.
	Version string `yaml:"version" json:"version"`
	// LastReviewed is the date a human last swept the whole registry.
	LastReviewed string     `yaml:"last_reviewed" json:"last_reviewed"`
	Maintainer   string     `yaml:"maintainer,omitempty" json:"maintainer,omitempty"`
	Providers    []Provider `yaml:"providers" json:"providers"`
}

// Registry is a loaded, validated registry indexed for lookup.
type Registry struct {
	File
	byID map[string]*Provider
}

// Load reads and validates a registry file.
func Load(path string) (*Registry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	return Parse(raw)
}

// Parse validates registry bytes. Split out from Load so tests need no files.
func Parse(raw []byte) (*Registry, error) {
	var f File
	// KnownFields would be stricter, but yaml.v3 only enforces it via a
	// decoder; unmarshalling here keeps unknown keys from being fatal while
	// Validate catches the fields that matter.
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}

	r := &Registry{File: f, byID: make(map[string]*Provider, len(f.Providers))}
	var errs []error
	for i := range r.Providers {
		p := &r.Providers[i]
		errs = append(errs, p.Validate()...)
		if _, dup := r.byID[p.ProviderID]; dup {
			errs = append(errs, fmt.Errorf("duplicate provider_id %q", p.ProviderID))
			continue
		}
		r.byID[p.ProviderID] = p
	}

	// Historical links must resolve, otherwise a migration chain silently
	// dangles and a predecessor looks deleted.
	for i := range r.Providers {
		p := &r.Providers[i]
		if p.HistoricalSuccessor != "" {
			if _, ok := r.byID[p.HistoricalSuccessor]; !ok {
				errs = append(errs, fmt.Errorf("%s: historical_successor %q is not in the registry",
					p.ProviderID, p.HistoricalSuccessor))
			}
		}
		if p.HistoricalPredecessor != "" {
			if _, ok := r.byID[p.HistoricalPredecessor]; !ok {
				errs = append(errs, fmt.Errorf("%s: historical_predecessor %q is not in the registry",
					p.ProviderID, p.HistoricalPredecessor))
			}
		}
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return r, nil
}

// Save writes a registry file back to disk. Used by verification to record
// last_verified and stale flags; it rewrites the whole document, so hand
// comments in the YAML are not preserved.
func Save(path string, f *File) error {
	out, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	header := "" +
		"# Machine-readable registry of research sources.\n" +
		"#\n" +
		"# This file is the source of truth. RESEARCH-INDEX.md is generated from\n" +
		"# it by `make research-index`; do not hand-edit the generated tables.\n" +
		"#\n" +
		"# Rights are data. An absent or unknown rights block is NOT permission.\n"
	if err := os.WriteFile(path, append([]byte(header), out...), 0o644); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	return nil
}

// Get returns a provider by id.
func (r *Registry) Get(id string) (*Provider, bool) {
	p, ok := r.byID[id]
	return p, ok
}

// Len reports how many providers are registered.
func (r *Registry) Len() int { return len(r.Providers) }

// ByStatus returns providers in a given lifecycle state, id-sorted.
func (r *Registry) ByStatus(s Status) []*Provider {
	var out []*Provider
	for i := range r.Providers {
		if r.Providers[i].Status == s {
			out = append(out, &r.Providers[i])
		}
	}
	sortProviders(out)
	return out
}

// ByType returns providers of a given class, id-sorted.
func (r *Registry) ByType(t ProviderType) []*Provider {
	var out []*Provider
	for i := range r.Providers {
		if r.Providers[i].Type == t {
			out = append(out, &r.Providers[i])
		}
	}
	sortProviders(out)
	return out
}

// Operational returns every provider the gateway may route traffic to.
func (r *Registry) Operational() []*Provider {
	var out []*Provider
	for i := range r.Providers {
		if r.Providers[i].Status.Operational() {
			out = append(out, &r.Providers[i])
		}
	}
	sortProviders(out)
	return out
}

// StatusCounts summarizes the backlog, which is what a coverage report wants.
func (r *Registry) StatusCounts() map[Status]int {
	counts := make(map[Status]int, len(Statuses))
	for i := range r.Providers {
		counts[r.Providers[i].Status]++
	}
	return counts
}

// Sections returns the RESEARCH-INDEX.md section keys present, in stable order.
func (r *Registry) Sections() []string {
	seen := map[string]bool{}
	var out []string
	for i := range r.Providers {
		s := r.Providers[i].Section
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// InSection returns the providers rendered under one section, in registry
// order so a maintainer controls the reading order of the generated tables.
func (r *Registry) InSection(section string) []*Provider {
	var out []*Provider
	for i := range r.Providers {
		if r.Providers[i].Section == section {
			out = append(out, &r.Providers[i])
		}
	}
	return out
}

// ReconcileRoutes checks that every route id claimed by the registry exists in
// the gateway's route configuration and vice versa. It is the guard against
// the registry and config/routes.yaml drifting apart.
//
// configuredRouteIDs is the set of route ids from config/routes.yaml.
func (r *Registry) ReconcileRoutes(configuredRouteIDs []string) []error {
	configured := make(map[string]bool, len(configuredRouteIDs))
	for _, id := range configuredRouteIDs {
		configured[id] = true
	}

	claimed := map[string]string{} // route id -> provider id
	var errs []error

	for i := range r.Providers {
		p := &r.Providers[i]
		for _, rid := range p.RouteIDs {
			if !configured[rid] {
				errs = append(errs, fmt.Errorf("%s: claims route %q which config/routes.yaml does not define",
					p.ProviderID, rid))
			}
			if other, dup := claimed[rid]; dup {
				errs = append(errs, fmt.Errorf("route %q is claimed by both %s and %s", rid, other, p.ProviderID))
			}
			claimed[rid] = p.ProviderID

			// A route pointed at a source we decided not to operate is the
			// failure this reconciliation exists to catch.
			if !p.Status.Operational() {
				errs = append(errs, fmt.Errorf("%s: is %s but serves route %q", p.ProviderID, p.Status, rid))
			}
		}
	}

	for _, rid := range configuredRouteIDs {
		if _, ok := claimed[rid]; !ok {
			errs = append(errs, fmt.Errorf("route %q has no provider in the registry", rid))
		}
	}
	return errs
}

func sortProviders(in []*Provider) {
	sort.Slice(in, func(i, j int) bool { return in[i].ProviderID < in[j].ProviderID })
}
