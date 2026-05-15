package config_test

import (
	"os"
	"testing"

	"aggregator/config"
)

func TestDefaults(t *testing.T) {
	cfg := config.Default()
	if cfg.Port != "8888" {
		t.Errorf("default port: got %q, want %q", cfg.Port, "8888")
	}
	if cfg.StaticDir != "static" {
		t.Errorf("default static_dir: got %q, want %q", cfg.StaticDir, "static")
	}
	if !cfg.EnableSamples {
		t.Error("default enable_samples should be true")
	}
}

const sampleYAML = `
# comment
port: 9090
static_dir: public
enable_samples: false
log_requests: true

datasets:
  - name: pefindo
    file: data/pefindo.json
  - name: credit
    file: data/credit.json
`

func TestLoad(t *testing.T) {
	f, err := os.CreateTemp("", "gator_cfg_*.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(sampleYAML)
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("port: got %q, want %q", cfg.Port, "9090")
	}
	if cfg.StaticDir != "public" {
		t.Errorf("static_dir: got %q, want %q", cfg.StaticDir, "public")
	}
	if cfg.EnableSamples {
		t.Error("enable_samples should be false")
	}
	if !cfg.LogRequests {
		t.Error("log_requests should be true")
	}
	if len(cfg.Datasets) != 2 {
		t.Fatalf("datasets: got %d, want 2", len(cfg.Datasets))
	}
	if cfg.Datasets[0].Name != "pefindo" || cfg.Datasets[0].File != "data/pefindo.json" {
		t.Errorf("datasets[0]: %+v", cfg.Datasets[0])
	}
	if cfg.Datasets[1].Name != "credit" || cfg.Datasets[1].File != "data/credit.json" {
		t.Errorf("datasets[1]: %+v", cfg.Datasets[1])
	}
	t.Logf("✓ config loaded: %+v", cfg)
}

func TestMissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/path/config.yml")
	if err == nil {
		t.Error("expected error for missing file")
	}
	t.Logf("✓ missing file returns error: %v", err)
}
