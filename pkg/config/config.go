// Package config loads and resolves gcphcpctl CLI configuration from its file,
// environment variables, and flags.
package config

import (
	"fmt"
	"os"
	"path/filepath"

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
	// APIEndpoint / OIDCEndpoint are explicit overrides from flags, environment
	// variables, or the config file. They take precedence over discovery.
	APIEndpoint  string `yaml:"api_endpoint,omitempty"`
	OIDCEndpoint string `yaml:"oidc_endpoint,omitempty"`
}

// UnmarshalYAML accepts "env" as an alias for the "environment" key so the
// config file matches the CLI's --env/--environment flag aliasing. The canonical
// "environment" key wins if both are present.
func (c *Config) UnmarshalYAML(value *yaml.Node) error {
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
func FromEnvironment(getenv func(string) string) Config {
	return Config{
		Project:      getenv("GCPHCPCTL_PROJECT"),
		Region:       getenv("GCPHCPCTL_REGION"),
		Environment:  getenv("GCPHCPCTL_ENVIRONMENT"),
		APIEndpoint:  getenv("GCPHCPCTL_API_ENDPOINT"),
		OIDCEndpoint: getenv("GCPHCPCTL_OIDC_ENDPOINT"),
	}
}

// Resolve merges CLI flags, environment variables, and the config file in
// descending priority. Empty values are absent, except an explicitly set output
// flag (including an empty value) takes precedence over lower layers.
func Resolve(flags, environment, file Config, outputFlagSet bool) Config {
	resolved := Config{
		Project:      firstNonEmpty(flags.Project, environment.Project, file.Project),
		Region:       firstNonEmpty(flags.Region, environment.Region, file.Region),
		Environment:  firstNonEmpty(flags.Environment, environment.Environment, file.Environment),
		APIEndpoint:  firstNonEmpty(flags.APIEndpoint, environment.APIEndpoint, file.APIEndpoint),
		OIDCEndpoint: firstNonEmpty(flags.OIDCEndpoint, environment.OIDCEndpoint, file.OIDCEndpoint),
		Output:       firstNonEmpty(file.Output, "text"),
	}
	if environment.Output != "" {
		resolved.Output = environment.Output
	}
	if outputFlagSet {
		resolved.Output = flags.Output
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
