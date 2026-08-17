package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// bashDefaultPattern matches ${VAR:-default} as used in routes.yaml. Go's
// stdlib os.ExpandEnv does not support the bash :- default syntax, so we
// preprocess the YAML source to resolve it before os.ExpandEnv runs.
var bashDefaultPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*):-([^}]*)\}`)

func expandEnvWithDefaults(s string) string {
	s = bashDefaultPattern.ReplaceAllStringFunc(s, func(match string) string {
		m := bashDefaultPattern.FindStringSubmatch(match)
		if len(m) != 3 {
			return match
		}
		varName, defaultVal := m[1], m[2]
		if v := os.Getenv(varName); v != "" {
			return v
		}
		return defaultVal
	})
	// Fall through to standard ${VAR} expansion for the remainder.
	return os.ExpandEnv(s)
}

// GatewayConfig is the top-level gateway configuration.
type GatewayConfig struct {
	Port             int           `yaml:"port"`
	RecipientAddress string        `yaml:"recipientAddress"`
	Network          string        `yaml:"network"`
	FacilitatorURL   string        `yaml:"facilitatorUrl"`
	DefaultPrice     string        `yaml:"defaultPrice"`
	Routes           []RouteConfig `yaml:"routes"`
	// Feed402 is the top-level feed402-protocol metadata. When present and
	// `Enabled` is true, the gateway serves /.well-known/feed402.json and
	// wraps all paid responses in the feed402 envelope (data + citation +
	// receipt). See SPEC at github.com/.../feed402.
	Feed402 Feed402Config `yaml:"feed402"`
}

// Feed402Config is the top-level feed402 provider metadata.
type Feed402Config struct {
	Enabled        bool   `yaml:"enabled"`
	Name           string `yaml:"name"`
	Version        string `yaml:"version"`
	Spec           string `yaml:"spec"`           // e.g. "feed402/0.2"
	CitationPolicy string `yaml:"citationPolicy"` // umbrella license for this provider
	Contact        string `yaml:"contact"`
	// Insight (optional) turns on the feed402 insight-tier endpoint. When
	// enabled the gateway registers a POST handler at Insight.Path that
	// accepts {question}, fans out to a configured retrieval route for
	// context, calls an LLM summarizer, and wraps the result in the §3
	// envelope with tier="insight".
	Insight InsightConfig `yaml:"insight"`
	// Resolve (optional) turns on the identity-resolution endpoint
	// (x402-research-gateway#5). When enabled the gateway registers a POST
	// handler at Resolve.Path that accepts {identifier}, fans out to every
	// route whose adapter implements IdentityProvider, and returns a
	// relation graph in the SPEC §3 envelope with one citation per
	// contributing provider.
	Resolve ResolveConfig `yaml:"resolve"`
	// Citations (optional) turns on the citation-graph endpoint
	// (x402-research-gateway#6). When enabled the gateway registers a POST
	// handler at Citations.Path that accepts {identifier, direction} and
	// returns normalized edges from every provider serving that direction,
	// with a per-provider account of what each one returned.
	Citations CitationsConfig `yaml:"citations"`
	// Federated (optional) turns on federated multi-provider search
	// (x402-research-gateway#4). POST at Federated.Path runs a paid
	// fan-out; GET at the same path is free and returns the cost estimate,
	// so an agent can price a call before paying for one.
	Federated FederatedConfig `yaml:"federated"`
}

// FederatedConfig configures the federated search endpoint.
type FederatedConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Path        string `yaml:"path"` // e.g. "/research/federated"
	Price       string `yaml:"price"`
	Description string `yaml:"description"`
	// ProviderRouteIDs restricts fan-out to these route ids. Empty means
	// every route whose adapter declares the requested capability.
	ProviderRouteIDs []string `yaml:"providerRouteIds"`
	// MaxConcurrency bounds simultaneous upstream calls. Default 4.
	MaxConcurrency int `yaml:"maxConcurrency"`
	// TimeoutSeconds is the fallback per-provider deadline for a route that
	// declares no upstream timeoutSeconds of its own. Default 10.
	TimeoutSeconds int `yaml:"timeoutSeconds"`
}

// CitationsConfig configures the citation-graph endpoint.
type CitationsConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Path        string `yaml:"path"` // e.g. "/research/citations"
	Price       string `yaml:"price"`
	Description string `yaml:"description"`
	// ProviderRouteIDs restricts fan-out to these route ids. Empty means
	// every route whose adapter implements CitationGraphProvider.
	ProviderRouteIDs []string `yaml:"providerRouteIds"`
	// MaxConcurrency bounds simultaneous upstream calls. Default 4.
	MaxConcurrency int `yaml:"maxConcurrency"`
	// TimeoutSeconds bounds each provider call. A provider that exceeds it
	// is reported as a timeout, never as a provider with no edges.
	// Default 10.
	TimeoutSeconds int `yaml:"timeoutSeconds"`
}

// ResolveConfig configures the identity-resolution endpoint.
type ResolveConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Path        string `yaml:"path"` // e.g. "/research/resolve"
	Price       string `yaml:"price"`
	Description string `yaml:"description"`
	// ProviderRouteIDs restricts fan-out to these route ids. Empty means
	// every route whose adapter implements IdentityProvider, which is the
	// useful default; naming ids is how an operator excludes a slow or
	// rate-limited upstream from the resolve path.
	ProviderRouteIDs []string `yaml:"providerRouteIds"`
	// MaxConcurrency bounds simultaneous upstream calls during fan-out.
	// Default 4.
	MaxConcurrency int `yaml:"maxConcurrency"`
	// TimeoutSeconds bounds each individual provider call. A provider that
	// exceeds it is reported as an explicit failure, never as zero
	// results. Default 10.
	TimeoutSeconds int `yaml:"timeoutSeconds"`
	// SimilarityThreshold is the minimum score at which a title/author
	// match is reported as possible_same_work. Zero uses the resolver
	// default.
	SimilarityThreshold float64 `yaml:"similarityThreshold"`
}

// InsightConfig configures the gateway's insight tier endpoint.
type InsightConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Path        string `yaml:"path"`  // e.g. "/research/insight"
	Price       string `yaml:"price"` // feed402 SPEC: insight is cheapest
	Description string `yaml:"description"`
	// RetrievalRouteID picks which existing search route the insight
	// handler fans out to for context. Must match a route.ID in Routes.
	RetrievalRouteID string `yaml:"retrievalRouteId"`
	// Summarizer: "mock" | "openai". Defaults to "mock" when the
	// OPENAI_API_KEY env var is not set so the gateway boots without keys.
	Summarizer string `yaml:"summarizer"`
	// Model: e.g. "gpt-4o-mini". Only used when Summarizer="openai".
	Model string `yaml:"model"`
	// MaxContextChars caps how much retrieval text is sent to the LLM to
	// bound cost; default 4000 (~1000 tokens).
	MaxContextChars int `yaml:"maxContextChars"`
}

// RouteConfig defines a single x402-protected research API route.
type RouteConfig struct {
	ID          string         `yaml:"id"`
	Path        string         `yaml:"path"`
	Method      string         `yaml:"method"`
	Description string         `yaml:"description"`
	MimeType    string         `yaml:"mimeType"`
	Price       string         `yaml:"price"`
	Upstream    UpstreamConfig `yaml:"upstream"`
	CacheTTL    int            `yaml:"cacheTtlSeconds"`

	// --- feed402 per-route metadata (all optional; required only when
	// GatewayConfig.Feed402.Enabled is true) ---

	// Feed402Tier classifies this route in the feed402 three-tier model.
	// Valid values: "raw", "query", "insight". Defaults to "query" if empty
	// on a Feed402-enabled gateway.
	Feed402Tier string `yaml:"feed402Tier"`
	// Citation is the provider's declared provenance for responses on this
	// route. The source_id template and canonical_url template are used to
	// build the citation block in the envelope. If neither template is set,
	// a generic provider-level citation is emitted (§3, type="source").
	Citation RouteCitation `yaml:"citation"`
}

// RouteCitation holds the citation configuration for a single route.
type RouteCitation struct {
	// SourcePrefix is the stable namespace of the source_id (e.g. "pubmed",
	// "openalex", "jackkruse"). Emitted as `<prefix>:<id>` when an id is
	// extracted, else as `<prefix>:query:<hash>` for search-tier responses.
	SourcePrefix string `yaml:"sourcePrefix"`
	// CanonicalURLTemplate is a go-template-ish string with `{id}` placeholder
	// where an extracted id should be substituted. Falls back to ProviderURL
	// for search responses.
	CanonicalURLTemplate string `yaml:"canonicalUrlTemplate"`
	// ProviderURL is the stable homepage of the upstream (e.g.
	// "https://pubmed.ncbi.nlm.nih.gov/"). Used for search-tier responses
	// where no single-source id is meaningful.
	ProviderURL string `yaml:"providerUrl"`
	// License is the per-route citation license (e.g. "CC-BY-4.0", "CC0",
	// "citation-only", "public"). Overrides Feed402Config.CitationPolicy.
	License string `yaml:"license"`
}

// UpstreamConfig defines how to proxy requests to an upstream API.
type UpstreamConfig struct {
	BaseURL      string            `yaml:"baseUrl"`
	Path         string            `yaml:"path"`
	PathTemplate string            `yaml:"pathTemplate"` // e.g., "/compound/name/{name}/JSON" — {param} substituted from query
	Method       string            `yaml:"method"`
	Headers      map[string]string `yaml:"headers"`
	QueryParams  map[string]string `yaml:"queryParams"`
	PassThrough  []string          `yaml:"passThrough"`
	Timeout      int               `yaml:"timeoutSeconds"`
}

// LoadFromFile loads gateway configuration from a YAML file, with env overrides.
func LoadFromFile(path string) (*GatewayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	// Expand environment variables in YAML (including bash ${VAR:-default}).
	expanded := expandEnvWithDefaults(string(data))

	var cfg GatewayConfig
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config YAML: %w", err)
	}

	// Apply env overrides
	if v := os.Getenv("PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Port = port
		}
	}
	if v := os.Getenv("RECIPIENT_ADDRESS"); v != "" {
		cfg.RecipientAddress = v
	}
	if v := os.Getenv("NETWORK"); v != "" {
		cfg.Network = v
	}
	if v := os.Getenv("FACILITATOR_URL"); v != "" {
		cfg.FacilitatorURL = v
	}

	// Apply defaults
	if cfg.Port == 0 {
		cfg.Port = 8091
	}
	if cfg.Network == "" {
		cfg.Network = "base-sepolia"
	}
	if cfg.FacilitatorURL == "" {
		cfg.FacilitatorURL = "https://facilitator.x402.rs"
	}
	if cfg.DefaultPrice == "" {
		cfg.DefaultPrice = "0.001"
	}

	for i := range cfg.Routes {
		if cfg.Routes[i].Method == "" {
			cfg.Routes[i].Method = "GET"
		}
		if cfg.Routes[i].MimeType == "" {
			cfg.Routes[i].MimeType = "application/json"
		}
		if cfg.Routes[i].Price == "" {
			cfg.Routes[i].Price = cfg.DefaultPrice
		}
		if cfg.Routes[i].Upstream.Timeout == 0 {
			cfg.Routes[i].Upstream.Timeout = 15
		}
		if cfg.Routes[i].Upstream.Method == "" {
			cfg.Routes[i].Upstream.Method = cfg.Routes[i].Method
		}
		// feed402 per-route defaults — only meaningful when Feed402.Enabled.
		if cfg.Routes[i].Feed402Tier == "" {
			cfg.Routes[i].Feed402Tier = "query"
		}
	}

	// feed402 top-level defaults.
	if cfg.Feed402.Enabled {
		if cfg.Feed402.Spec == "" {
			cfg.Feed402.Spec = "feed402/0.3"
		}
		if cfg.Feed402.Name == "" {
			cfg.Feed402.Name = "x402-research-gateway"
		}
		if cfg.Feed402.Version == "" {
			cfg.Feed402.Version = "0.1.0"
		}
		if cfg.Feed402.CitationPolicy == "" {
			cfg.Feed402.CitationPolicy = "mixed"
		}
		if cfg.Feed402.Insight.Enabled {
			if cfg.Feed402.Insight.Path == "" {
				cfg.Feed402.Insight.Path = "/research/insight"
			}
			if cfg.Feed402.Insight.Price == "" {
				cfg.Feed402.Insight.Price = "0.005"
			}
			if cfg.Feed402.Insight.Description == "" {
				cfg.Feed402.Insight.Description = "LLM-summarized research insight over paid retrieval"
			}
			if cfg.Feed402.Insight.Summarizer == "" {
				if os.Getenv("OPENAI_API_KEY") != "" {
					cfg.Feed402.Insight.Summarizer = "openai"
				} else {
					cfg.Feed402.Insight.Summarizer = "mock"
				}
			}
			if cfg.Feed402.Insight.Model == "" {
				cfg.Feed402.Insight.Model = "gpt-4o-mini"
			}
			if cfg.Feed402.Insight.MaxContextChars == 0 {
				cfg.Feed402.Insight.MaxContextChars = 4000
			}
		}
		if cfg.Feed402.Resolve.Enabled {
			if cfg.Feed402.Resolve.Path == "" {
				cfg.Feed402.Resolve.Path = "/research/resolve"
			}
			if cfg.Feed402.Resolve.Price == "" {
				cfg.Feed402.Resolve.Price = "0.005"
			}
			if cfg.Feed402.Resolve.Description == "" {
				cfg.Feed402.Resolve.Description = "Cross-provider scholarly identity resolution"
			}
			if cfg.Feed402.Resolve.MaxConcurrency == 0 {
				cfg.Feed402.Resolve.MaxConcurrency = 4
			}
			if cfg.Feed402.Resolve.TimeoutSeconds == 0 {
				cfg.Feed402.Resolve.TimeoutSeconds = 10
			}
		}
		if cfg.Feed402.Citations.Enabled {
			if cfg.Feed402.Citations.Path == "" {
				cfg.Feed402.Citations.Path = "/research/citations"
			}
			if cfg.Feed402.Citations.Price == "" {
				cfg.Feed402.Citations.Price = "0.005"
			}
			if cfg.Feed402.Citations.Description == "" {
				cfg.Feed402.Citations.Description = "Normalized citation graph across providers"
			}
			if cfg.Feed402.Citations.MaxConcurrency == 0 {
				cfg.Feed402.Citations.MaxConcurrency = 4
			}
			if cfg.Feed402.Citations.TimeoutSeconds == 0 {
				cfg.Feed402.Citations.TimeoutSeconds = 10
			}
		}
		if cfg.Feed402.Federated.Enabled {
			if cfg.Feed402.Federated.Path == "" {
				cfg.Feed402.Federated.Path = "/research/federated"
			}
			if cfg.Feed402.Federated.Price == "" {
				cfg.Feed402.Federated.Price = "0.005"
			}
			if cfg.Feed402.Federated.Description == "" {
				cfg.Feed402.Federated.Description = "Federated search across every provider declaring a capability"
			}
			if cfg.Feed402.Federated.MaxConcurrency == 0 {
				cfg.Feed402.Federated.MaxConcurrency = 4
			}
			if cfg.Feed402.Federated.TimeoutSeconds == 0 {
				cfg.Feed402.Federated.TimeoutSeconds = 10
			}
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate checks that the configuration is valid.
func (c *GatewayConfig) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Port)
	}
	if c.RecipientAddress == "" {
		return fmt.Errorf("recipientAddress is required (set RECIPIENT_ADDRESS env or in YAML)")
	}
	if len(c.RecipientAddress) != 42 || c.RecipientAddress[:2] != "0x" {
		return fmt.Errorf("invalid recipient address: %s", c.RecipientAddress)
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("at least one route is required")
	}
	seen := make(map[string]bool)
	for i, r := range c.Routes {
		if r.Path == "" {
			return fmt.Errorf("route[%d]: path is required", i)
		}
		if r.Upstream.BaseURL == "" {
			return fmt.Errorf("route[%d] %s: upstream.baseUrl is required", i, r.Path)
		}
		key := r.Method + " " + r.Path
		if seen[key] {
			return fmt.Errorf("duplicate route: %s", key)
		}
		seen[key] = true
		// feed402: validate tier if advertised.
		if c.Feed402.Enabled {
			switch r.Feed402Tier {
			case "raw", "query", "insight":
			default:
				return fmt.Errorf("route[%d] %s: feed402Tier must be raw|query|insight, got %q", i, r.Path, r.Feed402Tier)
			}
		}
	}
	return nil
}

// CAIP2Network returns the CAIP-2 formatted network identifier.
func (c *GatewayConfig) CAIP2Network() string {
	switch c.Network {
	case "base":
		return "eip155:8453"
	case "base-sepolia":
		return "eip155:84532"
	default:
		return c.Network
	}
}
