// Package config loads and resolves gcphcpctl CLI configuration from its file,
// environment variables, and flags.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config holds CLI settings from a source or the resolved effective values.
// omitempty keeps unset fields out of marshaled output (e.g. `gcphcpctl config`);
// it does not affect loading.
type Config struct {
	Project     string `yaml:"project,omitempty"`
	Region      string `yaml:"region,omitempty"`
	Environment string `yaml:"environment,omitempty"`
	Output      string `yaml:"output,omitempty"`
	Verbosity   int    `yaml:"verbosity,omitempty"`
	// verbositySet distinguishes an explicit zero environment value from absence.
	verbositySet bool
	// APIEndpoint / OIDCEndpoint are explicit overrides from flags, environment
	// variables, or the config file. They take precedence over discovery.
	APIEndpoint  string `yaml:"api_endpoint,omitempty"`
	OIDCEndpoint string `yaml:"oidc_endpoint,omitempty"`
}

// UnmarshalYAML accepts "env" as an alias for the "environment" key so the
// config file matches the CLI's --env/--environment flag aliasing. The canonical
// "environment" key wins if both are present.
func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	// yaml.v3 otherwise coerces fractional numbers into ints. Decode the field's
	// node separately to validate its type, including values inherited by merges.
	var verbosity struct {
		Value yaml.Node `yaml:"verbosity"`
	}
	if err := value.Decode(&verbosity); err != nil {
		return err
	}
	node := &verbosity.Value
	for node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	if node.Kind != 0 && node.Tag != "!!int" {
		return fmt.Errorf("verbosity must be a nonnegative integer")
	}
	// rawConfig avoids infinite recursion back into this method; the inline
	// embed pulls in every canonical field, and Env captures the alias key.
	type rawConfig Config
	aux := struct {
		rawConfig `yaml:",inline"`
		Env       string `yaml:"env"`
	}{}
	if err := value.Decode(&aux); err != nil {
		return err
	}
	if aux.Verbosity < 0 {
		return fmt.Errorf("verbosity must be a nonnegative integer")
	}
	*c = Config(aux.rawConfig)
	if c.Environment == "" {
		c.Environment = aux.Env
	}
	return nil
}

// DefaultConfigDir returns the default config directory path.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gcphcpctl")
}

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() string {
	dir := DefaultConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.yaml")
}

// Load reads configuration from the given path. If the file does not exist,
// it returns an empty Config without error. Returns an error only if the file
// exists but cannot be parsed.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	if path == "" {
		return &Config{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	return &cfg, nil
}

// FromEnvironment reads the supported CLI environment variables. Passing the
// lookup function keeps environment reads separate from the precedence rules.
func FromEnvironment(getenv func(string) string) (Config, error) {
	cfg := Config{
		Project:      getenv("GCPHCPCTL_PROJECT"),
		Region:       getenv("GCPHCPCTL_REGION"),
		Environment:  getenv("GCPHCPCTL_ENVIRONMENT"),
		APIEndpoint:  getenv("GCPHCPCTL_API_ENDPOINT"),
		OIDCEndpoint: getenv("GCPHCPCTL_OIDC_ENDPOINT"),
	}
	if value := getenv("GCPHCPCTL_VERBOSITY"); value != "" {
		verbosity, err := strconv.Atoi(value)
		if err != nil || verbosity < 0 {
			// Atoi's error includes the raw value, which could be a secret.
			return Config{}, fmt.Errorf("GCPHCPCTL_VERBOSITY must be a nonnegative integer")
		}
		cfg.Verbosity = verbosity
		cfg.verbositySet = true
	}
	return cfg, nil
}

// Resolve merges CLI flags, environment variables, and the config file in
// descending priority. Empty strings are absent, except an explicitly set output
// flag (including an empty value). An explicitly set verbosity flag or environment
// value takes precedence even when zero.
func Resolve(flags, environment, file Config, outputFlagSet, verbosityFlagSet bool) Config {
	resolved := Config{
		Project:      firstNonEmpty(flags.Project, environment.Project, file.Project),
		Region:       firstNonEmpty(flags.Region, environment.Region, file.Region),
		Environment:  firstNonEmpty(flags.Environment, environment.Environment, file.Environment),
		APIEndpoint:  firstNonEmpty(flags.APIEndpoint, environment.APIEndpoint, file.APIEndpoint),
		OIDCEndpoint: firstNonEmpty(flags.OIDCEndpoint, environment.OIDCEndpoint, file.OIDCEndpoint),
		Output:       firstNonEmpty(file.Output, "text"),
		Verbosity:    file.Verbosity,
	}
	if environment.Output != "" {
		resolved.Output = environment.Output
	}
	if outputFlagSet {
		resolved.Output = flags.Output
	}
	if environment.verbositySet {
		resolved.Verbosity = environment.Verbosity
	}
	if verbosityFlagSet {
		resolved.Verbosity = flags.Verbosity
	}
	return resolved
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
