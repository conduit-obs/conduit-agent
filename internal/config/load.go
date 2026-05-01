package config

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads conduit.yaml from disk, applies defaults, and runs Validate.
// It returns a fully canonical AgentConfig ready for the expander to consume,
// or a single, structured error describing every problem found.
func Load(path string) (*AgentConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open conduit config %q: %w", path, err)
	}
	defer f.Close()

	cfg, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return cfg, nil
}

// Parse reads YAML bytes from r, applies defaults, and runs Validate.
// Useful for tests and for piping conduit.yaml from stdin (FR-12 preview path).
func Parse(r io.Reader) (*AgentConfig, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	var cfg AgentConfig
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode yaml: %w", err)
	}

	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// applyDefaults fills in any optional fields the user omitted. Keep this
// function minimal: only fields with a single uncontroversial default belong
// here. Anything that needs a policy decision goes through Validate.
func (c *AgentConfig) applyDefaults() {
	if c.Output.Mode == OutputModeHoneycomb && c.Output.Honeycomb != nil {
		if c.Output.Honeycomb.Endpoint == "" {
			c.Output.Honeycomb.Endpoint = DefaultHoneycombEndpoint
		}
	}

	// A missing profile block means "platform defaults on, auto-detect OS".
	// We materialize the struct so downstream consumers can read Mode without
	// nil-checks.
	if c.Profile == nil {
		c.Profile = &Profile{Mode: ProfileModeAuto}
	} else if c.Profile.Mode == "" {
		c.Profile.Mode = ProfileModeAuto
	}
}
