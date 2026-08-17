package provider

import "github.com/gianyrox/x402-research-gateway/internal/config"

// testRoute builds a minimal RouteConfig carrying only what
// GenericCitationProvider reads: ID and Citation.SourcePrefix.
func testRoute(id, sourcePrefix string) *config.RouteConfig {
	return &config.RouteConfig{
		ID:       id,
		Citation: config.RouteCitation{SourcePrefix: sourcePrefix},
	}
}
