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
	defer func() { _ = f.Close() }()

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

	// PersistentQueue.Dir defaults to DefaultPersistentQueueDir when
	// the queue is enabled and the operator left Dir empty (M10.A).
	// We don't materialize the block when Enabled is false because the
	// downstream expander branches on `pq != nil && pq.Enabled` —
	// keeping the nil case meaningful avoids spurious "queue disabled
	// but dir set" config drift.
	if pq := c.Output.PersistentQueue; pq != nil && pq.Enabled && pq.Dir == "" {
		pq.Dir = DefaultPersistentQueueDir
	}

	// A missing profile block means "platform defaults on, auto-detect OS".
	// We materialize the struct so downstream consumers can read Mode without
	// nil-checks.
	if c.Profile == nil {
		c.Profile = &Profile{Mode: ProfileModeAuto}
	} else if c.Profile.Mode == "" {
		c.Profile.Mode = ProfileModeAuto
	}

	// A missing metrics.red block means "RED enabled, default dimensions,
	// default cardinality limit". Materializing the struct keeps the
	// expander's branches symmetric with the profile / overrides paths
	// (no nil-check, just method calls).
	if c.Metrics == nil {
		c.Metrics = &Metrics{}
	}
	if c.Metrics.RED == nil {
		c.Metrics.RED = &REDConfig{}
	}
	if c.Metrics.RED.CardinalityLimit == 0 {
		c.Metrics.RED.CardinalityLimit = DefaultREDCardinalityLimit
	}

	// OBI defaults: pre-fill Enabled per the profile so downstream
	// readers (expander, doctor, helm-chart docs) always see a
	// concrete bool rather than "depends on profile". K8s defaults
	// to true; every other resolved profile defaults to false. We
	// materialize the OBI struct itself when omitted so the
	// expander's branch on cfg.OBI != nil remains a stable contract
	// across "operator left it out" and "operator wrote `obi:
	// {enabled: false}`". See ADR-0020 sub-decision 4.
	if c.OBI == nil {
		c.OBI = &OBI{}
	}
	if c.OBI.Enabled == nil {
		def := obiDefaultForProfile(c.Profile)
		c.OBI.Enabled = &def
	}
}
