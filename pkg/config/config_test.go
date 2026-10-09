package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("project: my-project\nregion: us-east1\noutput: json\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Project != "my-project" {
		t.Errorf("expected project 'my-project', got %q", cfg.Project)
	}
	if cfg.Region != "us-east1" {
		t.Errorf("expected region 'us-east1', got %q", cfg.Region)
	}
	if cfg.Output != "json" {
		t.Errorf("expected output 'json', got %q", cfg.Output)
	}
}

func TestLoad_APIConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "api_endpoint: https://api.example.com\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.APIEndpoint != "https://api.example.com" {
		t.Errorf("expected api_endpoint 'https://api.example.com', got %q", cfg.APIEndpoint)
	}
}

func TestLoad_RegionAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "region: us-central1\nenvironment: integration\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Region != "us-central1" {
		t.Errorf("expected region 'us-central1', got %q", cfg.Region)
	}
	if cfg.Environment != "integration" {
		t.Errorf("expected environment 'integration', got %q", cfg.Environment)
	}
}

func TestLoad_PartialConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("project: only-project\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Project != "only-project" {
		t.Errorf("expected project 'only-project', got %q", cfg.Project)
	}
	if cfg.Region != "" {
		t.Errorf("expected empty region, got %q", cfg.Region)
	}
	if cfg.Output != "" {
		t.Errorf("expected empty output, got %q", cfg.Output)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg.Project != "" || cfg.Region != "" {
		t.Error("expected empty config for missing file")
	}
}

func TestLoad_EmptyPath(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("{{invalid yaml:::"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoad_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Project != "" || cfg.Region != "" {
		t.Error("expected empty config for empty file")
	}
}

func TestLoad_ExtraFieldsIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "project: p1\nregion: r1\nunknown_field: value\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Project != "p1" {
		t.Errorf("expected project 'p1', got %q", cfg.Project)
	}
}

func TestDefaultConfigDir(t *testing.T) {
	dir := DefaultConfigDir()
	if dir == "" {
		t.Skip("could not determine home directory")
	}
	if filepath.Base(dir) != ".gcphcpctl" {
		t.Errorf("expected dir to end with '.gcphcpctl', got %q", dir)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	path := DefaultConfigPath()
	if path == "" {
		t.Skip("could not determine home directory")
	}
	if filepath.Base(path) != "config.yaml" {
		t.Errorf("expected path to end with 'config.yaml', got %q", path)
	}
}

func TestLoad_EnvAlias(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"env alias key", "env: dev\n", "dev"},
		{"canonical environment key", "environment: integration\n", "integration"},
		{"canonical wins when both set", "environment: integration\nenv: dev\n", "integration"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tc.content), 0644); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Environment != tc.want {
				t.Errorf("expected environment %q, got %q", tc.want, cfg.Environment)
			}
		})
	}
}

func TestResolvePrecedence(t *testing.T) {
	file := Config{Project: "file-project", Region: "file-region", Environment: "dev", Output: "json", APIEndpoint: "file-api", OIDCEndpoint: "file-oidc"}
	environment := Config{Project: "env-project", Environment: "stage", APIEndpoint: "env-api"}
	flags := Config{Project: "flag-project", Region: "flag-region", OIDCEndpoint: "flag-oidc"}

	got := Resolve(flags, environment, file, false)
	want := Config{Project: "flag-project", Region: "flag-region", Environment: "stage", Output: "json", APIEndpoint: "env-api", OIDCEndpoint: "flag-oidc"}
	if got != want {
		t.Errorf("Resolve() = %+v, want %+v", got, want)
	}
	if file.Project != "file-project" || environment.Project != "env-project" || flags.Project != "flag-project" {
		t.Fatal("Resolve changed a source value")
	}
}

func TestResolveOutput(t *testing.T) {
	file := Config{Output: "json"}
	if got := Resolve(Config{Output: "yaml"}, Config{}, file, false).Output; got != "json" {
		t.Errorf("unchanged output flag should use config file, got %q", got)
	}
	if got := Resolve(Config{Output: "yaml"}, Config{}, file, true).Output; got != "yaml" {
		t.Errorf("explicit output flag should win, got %q", got)
	}
	if got := Resolve(Config{}, Config{}, file, true).Output; got != "" {
		t.Errorf("explicit empty output flag should win, got %q", got)
	}
	if got := Resolve(Config{}, Config{}, Config{}, false).Output; got != "text" {
		t.Errorf("default output should be text, got %q", got)
	}
}

func TestFromEnvironment(t *testing.T) {
	values := map[string]string{
		"GCPHCPCTL_PROJECT":       "project",
		"GCPHCPCTL_REGION":        "region",
		"GCPHCPCTL_ENVIRONMENT":   "environment",
		"GCPHCPCTL_API_ENDPOINT":  "api",
		"GCPHCPCTL_OIDC_ENDPOINT": "oidc",
	}
	got := FromEnvironment(func(name string) string { return values[name] })
	want := Config{Project: "project", Region: "region", Environment: "environment", APIEndpoint: "api", OIDCEndpoint: "oidc"}
	if got != want {
		t.Errorf("FromEnvironment() = %+v, want %+v", got, want)
	}
}
