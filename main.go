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
	"sort"
	"strings"
	"sync"
	"time"

	"aggregator/config"
	"aggregator/gator"
	"aggregator/gator/ingest"
	"aggregator/gator/samples"
)

// xsdStoreT is a thread-safe store for XSD hints, keyed by dataset name.
type xsdStoreT struct {
	mu    sync.RWMutex
	hints map[string]ingest.XSDHints
}

func newXSDStore() *xsdStoreT {
	return &xsdStoreT{hints: map[string]ingest.XSDHints{}}
}

func (x *xsdStoreT) Set(name string, h ingest.XSDHints) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.hints[name] = h
}

func (x *xsdStoreT) Get(name string) (ingest.XSDHints, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	h, ok := x.hints[name]
	return h, ok
}

func (x *xsdStoreT) Has(name string) bool {
	x.mu.RLock()
	defer x.mu.RUnlock()
	_, ok := x.hints[name]
	return ok
}

func (x *xsdStoreT) Names() []string {
	x.mu.RLock()
	defer x.mu.RUnlock()
	names := make([]string, 0, len(x.hints))
	for k := range x.hints {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// ===================== Server =====================

// server bundles the dataset store and config so handlers can share them.
type server struct {
	store    *gator.Store
	xsdStore *xsdStoreT
	cfg      config.Config
	logger   *log.Logger
}

func newServer(cfg config.Config) *server {
	return &server{
		store:    gator.NewStore(),
		xsdStore: newXSDStore(),
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
	writeJSON(w, gator.DetectSchemaFromSample(data))
}

func (s *server) handleData(w http.ResponseWriter, r *http.Request) {
	dataset := r.URL.Query().Get("dataset")
	data, _ := s.store.Get(dataset)
	writeJSON(w, data)
}

// withCORS adds CORS headers and handles OPTIONS preflight requests.
// This allows the frontend to be hosted on a different origin than the API.
func withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

func (s *server) handleAggregate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10 MB
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
	rows, err := gator.Aggregate(s.store, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, rows)
}

func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20) // 50 MB
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
			if hints, ok := s.xsdStore.Get(hintDataset); ok {
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

	// Normalise XSD paths: strip the root element prefix so they match the
	// schema paths produced by DetectSchemaFromSample (which sees data[0] after
	// ParseXML has unwrapped the root element).
	//
	// Example: XSD builds "Response.Product.TradeLines.TradeLine" because it
	// sees the root <Response> element.  But ParseXML returns data[0] as the
	// contents of <Response>, so DetectSchema sees "Product.TradeLines.TradeLine".
	// Stripping one leading segment makes both paths identical.
	hints = ingest.StripRootPrefix(hints)

	s.xsdStore.Set(dataset, hints)
	writeJSON(w, map[string]interface{}{
		"dataset":     dataset,
		"stringPaths": len(hints.StringPaths),
		"arrayPaths":  len(hints.ArrayPaths),
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
	writeJSON(w, map[string]interface{}{"xsdDatasets": s.xsdStore.Names()})
}

// handleXSDInfo returns the hints for a specific dataset.
func (s *server) handleXSDInfo(w http.ResponseWriter, r *http.Request) {
	dataset := r.URL.Query().Get("dataset")
	hints, ok := s.xsdStore.Get(dataset)
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
// If xsdFile is non-empty and the file is XML, XSD hints are parsed and returned.
// The returned *XSDHints is nil when no XSD is used or the file is JSON.
func loadFileDataset(path string, xsdFile string) ([]interface{}, *ingest.XSDHints, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	isXML := strings.HasSuffix(strings.ToLower(path), ".xml") ||
		(len(b) > 0 && b[0] == '<')

	if isXML {
		opts := ingest.DefaultXMLOptions()
		var hints *ingest.XSDHints
		if xsdFile != "" {
			xb, err := os.ReadFile(xsdFile)
			if err != nil {
				log.Printf("xsd_file %q: read failed: %v (ignoring)", xsdFile, err)
			} else {
				h, err := ingest.ParseXSD(bytes.NewReader(xb))
				if err != nil {
					log.Printf("xsd_file %q: parse failed: %v (ignoring)", xsdFile, err)
				} else {
					h = ingest.StripRootPrefix(h)
					opts.Hints = h
					hints = &h
				}
			}
		}
		data, err := ingest.ParseXML(bytes.NewReader(b), opts)
		return data, hints, err
	}

	var parsed interface{}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, nil, err
	}
	switch v := parsed.(type) {
	case []interface{}:
		return v, nil, nil
	default:
		return []interface{}{v}, nil, nil
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
		data, hints, err := loadFileDataset(ds.File, ds.XSDFile)
		if err != nil {
			log.Printf("dataset %q: load %q failed: %v", ds.Name, ds.File, err)
			continue
		}
		srv.store.Register(ds.Name, data)
		if hints != nil {
			srv.xsdStore.Set(ds.Name, *hints)
			log.Printf("dataset %q: loaded %d records from %q (XSD hints applied)", ds.Name, len(data), ds.File)
		} else {
			log.Printf("dataset %q: loaded %d records from %q", ds.Name, len(data), ds.File)
		}
	}

	// Set up routes
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(cfg.StaticDir)))
	mux.HandleFunc("/api/datasets", withCORS(srv.handleListDatasets))
	mux.HandleFunc("/api/schema", withCORS(srv.handleSchema))
	mux.HandleFunc("/api/data", withCORS(srv.handleData))
	mux.HandleFunc("/api/aggregate", withCORS(srv.handleAggregate))
	mux.HandleFunc("/api/upload", withCORS(srv.handleUpload))
	mux.HandleFunc("/api/upload/xsd", withCORS(srv.handleUploadXSD))
	mux.HandleFunc("/api/xsd", withCORS(srv.handleListXSD))
	mux.HandleFunc("/api/xsd/info", withCORS(srv.handleXSDInfo))

	handler := srv.logging(mux)

	log.Printf("Visual Aggregator listening on :%s  (static: %q)", cfg.Port, cfg.StaticDir)
	if err := http.ListenAndServe(":"+cfg.Port, handler); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
