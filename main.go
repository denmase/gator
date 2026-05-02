// Visual Aggregator — HTTP server.
//
// Usage:
//
//	go run main.go [config.yml]
//
// The optional argument overrides the default config file path ("config.yml").
// If the config file is not found, built-in defaults are used and the built-in
// sample datasets are registered automatically.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"aggregator/config"
	"aggregator/gator"
	"aggregator/gator/ingest"
	"aggregator/gator/samples"
)

// ===================== Server =====================

// server bundles the dataset store and config so handlers can share them.
type server struct {
	store    *gator.Store
	xsdStore map[string]ingest.XSDHints // dataset name → parsed XSD hints
	cfg      config.Config
	logger   *log.Logger
}

func newServer(cfg config.Config) *server {
	return &server{
		store:    gator.NewStore(),
		xsdStore: map[string]ingest.XSDHints{},
		cfg:      cfg,
		logger:   log.New(os.Stdout, "[gator] ", log.LstdFlags),
	}
}

// ===================== HTTP Handlers =====================

func (s *server) handleListDatasets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"datasets": s.store.Names()})
}

func (s *server) handleSchema(w http.ResponseWriter, r *http.Request) {
	dataset := r.URL.Query().Get("dataset")
	data, ok := s.store.Get(dataset)
	if !ok || len(data) == 0 {
		writeJSON(w, []gator.FieldInfo{})
		return
	}
	writeJSON(w, gator.DetectSchema(data[0], "", ""))
}

func (s *server) handleData(w http.ResponseWriter, r *http.Request) {
	dataset := r.URL.Query().Get("dataset")
	data, _ := s.store.Get(dataset)
	writeJSON(w, data)
}

func (s *server) handleAggregate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	var req gator.AggregateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, gator.Aggregate(s.store, req))
}

func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	ct := r.Header.Get("Content-Type")
	isXML := strings.Contains(ct, "xml") || (len(body) > 0 && body[0] == '<')

	// Optional: caller may pass ?dataset=<name> to attach XSD hints from a
	// previously uploaded XSD that was stored under the same name.
	hintDataset := r.URL.Query().Get("dataset")

	var data []interface{}
	if isXML {
		opts := ingest.DefaultXMLOptions()
		if hintDataset != "" {
			if hints, ok := s.xsdStore[hintDataset]; ok {
				opts.Hints = hints
			}
		}
		data, err = ingest.ParseXML(bytes.NewReader(body), opts)
		if err != nil {
			http.Error(w, "invalid XML: "+err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		var parsed interface{}
		if err := json.Unmarshal(body, &parsed); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		switch v := parsed.(type) {
		case []interface{}:
			data = v
		default:
			data = []interface{}{v}
		}
	}

	name := fmt.Sprintf("uploaded_%d", time.Now().UnixNano())
	s.store.Register(name, data)

	format := "json"
	if isXML {
		format = "xml"
	}
	writeJSON(w, map[string]interface{}{
		"name":   name,
		"count":  len(data),
		"format": format,
	})
}

// handleUploadXSD accepts an XSD file and stores the parsed hints under a
// dataset name provided in the query parameter ?dataset=<name>.
// After uploading data and its XSD separately, the hints are applied when
// re-uploading XML with ?dataset=<name>, or can be applied retroactively
// via /api/reparse?dataset=<name>.
func (s *server) handleUploadXSD(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dataset := r.URL.Query().Get("dataset")
	if dataset == "" {
		http.Error(w, "missing ?dataset= parameter", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	hints, err := ingest.ParseXSD(bytes.NewReader(body))
	if err != nil {
		http.Error(w, "invalid XSD: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.xsdStore[dataset] = hints
	writeJSON(w, map[string]interface{}{
		"dataset":      dataset,
		"stringPaths":  len(hints.StringPaths),
		"arrayPaths":   len(hints.ArrayPaths),
	})
}

// handleReparse re-parses an already-uploaded XML dataset using the XSD hints
// that were uploaded for it. This allows applying XSD hints without re-uploading
// the XML file. Only works if the original raw bytes were XML; JSON datasets
// are unaffected.
func (s *server) handleReparse(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "reparse requires raw bytes — upload XML again with ?dataset=<xsd_name>", http.StatusNotImplemented)
}

// handleListXSD returns the set of dataset names that have XSD hints loaded.
func (s *server) handleListXSD(w http.ResponseWriter, r *http.Request) {
	names := make([]string, 0, len(s.xsdStore))
	for k := range s.xsdStore {
		names = append(names, k)
	}
	writeJSON(w, map[string]interface{}{"xsdDatasets": names})
}

// handleXSDInfo returns the hints for a specific dataset.
func (s *server) handleXSDInfo(w http.ResponseWriter, r *http.Request) {
	dataset := r.URL.Query().Get("dataset")
	hints, ok := s.xsdStore[dataset]
	if !ok {
		writeJSON(w, map[string]interface{}{"stringPaths": []string{}, "arrayPaths": []string{}})
		return
	}
	sp := make([]string, 0, len(hints.StringPaths))
	for k := range hints.StringPaths {
		sp = append(sp, k)
	}
	ap := make([]string, 0, len(hints.ArrayPaths))
	for k := range hints.ArrayPaths {
		ap = append(ap, k)
	}
	writeJSON(w, map[string]interface{}{"stringPaths": sp, "arrayPaths": ap})
}

// ===================== Middleware =====================

func (s *server) logging(next http.Handler) http.Handler {
	if !s.cfg.LogRequests {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

// ===================== Helpers =====================

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// loadFileDataset reads a JSON or XML file and returns its contents as []interface{}.
// Format is detected by file extension (.xml) or content sniffing.
func loadFileDataset(path string) ([]interface{}, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	isXML := strings.HasSuffix(strings.ToLower(path), ".xml") ||
		(len(b) > 0 && b[0] == '<')

	if isXML {
		return ingest.ParseXML(bytes.NewReader(b), ingest.DefaultXMLOptions())
	}

	var parsed interface{}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, err
	}
	switch v := parsed.(type) {
	case []interface{}:
		return v, nil
	default:
		return []interface{}{v}, nil
	}
}

// ===================== Main =====================

func main() {
	// Resolve config file path
	cfgPath := "config.yml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	// Load config (falls back to defaults if file is missing)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("config file %q not found, using defaults", cfgPath)
			cfg = config.Default()
		} else {
			log.Fatalf("config error: %v", err)
		}
	}

	srv := newServer(cfg)

	// Register built-in sample datasets
	if cfg.EnableSamples {
		samples.Register(srv.store)
		log.Printf("registered built-in sample datasets")
	}

	// Register file-backed datasets from config
	for _, ds := range cfg.Datasets {
		if ds.File == "" {
			log.Printf("dataset %q: no file specified, skipping", ds.Name)
			continue
		}
		data, err := loadFileDataset(ds.File)
		if err != nil {
			log.Printf("dataset %q: load %q failed: %v", ds.Name, ds.File, err)
			continue
		}
		srv.store.Register(ds.Name, data)
		log.Printf("dataset %q: loaded %d records from %q", ds.Name, len(data), ds.File)
	}

	// Set up routes
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(cfg.StaticDir)))
	mux.HandleFunc("/api/datasets", srv.handleListDatasets)
	mux.HandleFunc("/api/schema", srv.handleSchema)
	mux.HandleFunc("/api/data", srv.handleData)
	mux.HandleFunc("/api/aggregate", srv.handleAggregate)
	mux.HandleFunc("/api/upload", srv.handleUpload)
	mux.HandleFunc("/api/upload/xsd", srv.handleUploadXSD)
	mux.HandleFunc("/api/xsd", srv.handleListXSD)
	mux.HandleFunc("/api/xsd/info", srv.handleXSDInfo)

	handler := srv.logging(mux)

	log.Printf("Visual Aggregator listening on :%s  (static: %q)", cfg.Port, cfg.StaticDir)
	if err := http.ListenAndServe(":"+cfg.Port, handler); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
