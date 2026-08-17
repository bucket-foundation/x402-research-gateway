package registry

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// routesDoc is the slice of config/routes.yaml the registry cares about. It
// deliberately reads only route ids rather than the full route config, so the
// registry stays independent of how routes are otherwise shaped.
type routesDoc struct {
	Routes []struct {
		ID string `yaml:"id"`
	} `yaml:"routes"`
}

// RouteIDsFromConfig reads the configured route ids from the gateway's route
// configuration, for reconciliation against the registry.
func RouteIDsFromConfig(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read routes: %w", err)
	}
	var doc routesDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse routes: %w", err)
	}
	out := make([]string, 0, len(doc.Routes))
	for _, r := range doc.Routes {
		if r.ID != "" {
			out = append(out, r.ID)
		}
	}
	return out, nil
}
