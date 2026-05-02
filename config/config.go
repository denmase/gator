// Package config loads server configuration from a YAML file.
// It uses only the Go standard library (no external YAML dependency) by
// implementing a simple line-by-line key: value parser that covers the
// subset of YAML needed for this application.
//
// Supported syntax:
//
//	key: value          — top-level scalar
//	datasets:           — section header
//	  - name: foo       — list item with sub-keys
//	    file: bar.json
//	  - name: baz
//	    file: qux.json
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// DatasetEntry configures one pre-loaded dataset.
type DatasetEntry struct {
	// Name is the identifier used in the API (e.g. "employees").
	Name string
	// File is the path to a JSON file to load.  If empty the dataset is not
	// file-backed (it may be registered programmatically, e.g. built-in samples).
	File string
}

// Config holds the full server configuration.
type Config struct {
	// Port to listen on, e.g. "8888".
	Port string
	// StaticDir is the directory served as the web root for index.html, etc.
	StaticDir string
	// Datasets lists file-backed datasets to pre-load at startup.
	Datasets []DatasetEntry
	// EnableSamples controls whether the built-in sample datasets are registered.
	EnableSamples bool
	// LogRequests enables basic request logging to stdout.
	LogRequests bool
}

// Default returns a Config populated with sensible defaults.
func Default() Config {
	return Config{
		Port:          "8888",
		StaticDir:     "static",
		EnableSamples: true,
		LogRequests:   false,
	}
}

// Load reads path and returns a Config.  Missing keys fall back to Default().
// Unknown keys are silently ignored so future additions are backward-compatible.
func Load(path string) (Config, error) {
	cfg := Default()

	f, err := os.Open(path)
	if err != nil {
		return cfg, fmt.Errorf("config: open %q: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	// Simple state machine: track whether we are inside the "datasets:" list.
	inDatasets := false
	var current *DatasetEntry

	flush := func() {
		if current != nil && current.Name != "" {
			cfg.Datasets = append(cfg.Datasets, *current)
		}
		current = nil
	}

	for scanner.Scan() {
		raw := scanner.Text()
		// Remove inline comments
		if idx := strings.Index(raw, " #"); idx >= 0 {
			raw = raw[:idx]
		}
		line := strings.TrimRight(raw, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}

		indent := leadingSpaces(line)
		trimmed := strings.TrimSpace(line)

		// ── Top-level keys (indent == 0) ───────────────────────────────────
		if indent == 0 {
			flush()
			inDatasets = false

			key, val := splitKV(trimmed)
			switch key {
			case "port":
				cfg.Port = val
			case "static_dir":
				cfg.StaticDir = val
			case "enable_samples":
				cfg.EnableSamples = parseBool(val)
			case "log_requests":
				cfg.LogRequests = parseBool(val)
			case "datasets":
				inDatasets = true
			}
			continue
		}

		// ── Dataset list items (inside "datasets:") ────────────────────────
		if inDatasets {
			if strings.HasPrefix(trimmed, "- ") {
				flush()
				current = &DatasetEntry{}
				// Could have "- name: foo" on the same line
				sub := strings.TrimPrefix(trimmed, "- ")
				k, v := splitKV(sub)
				setDatasetField(current, k, v)
			} else {
				// Continuation of current item
				k, v := splitKV(trimmed)
				if current == nil {
					current = &DatasetEntry{}
				}
				setDatasetField(current, k, v)
			}
		}
	}
	flush()

	if err := scanner.Err(); err != nil {
		return cfg, fmt.Errorf("config: scan %q: %w", path, err)
	}
	return cfg, nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func splitKV(s string) (key, val string) {
	idx := strings.IndexByte(s, ':')
	if idx < 0 {
		return strings.TrimSpace(s), ""
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:])
}

func leadingSpaces(s string) int {
	n := 0
	for _, c := range s {
		if c == ' ' {
			n++
		} else if c == '\t' {
			n += 2
		} else {
			break
		}
	}
	return n
}

func parseBool(s string) bool {
	return s == "true" || s == "yes" || s == "1"
}

func setDatasetField(e *DatasetEntry, key, val string) {
	switch key {
	case "name":
		e.Name = val
	case "file":
		e.File = val
	}
}
