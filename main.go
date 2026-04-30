package main

import (
"encoding/json"
"fmt"
"io"
"log"
"net/http"
"os"
"path/filepath"
"strings"

"visual-aggregator/pkg/aggregator"
)

func main() {
// Setup static file server
staticDir := "./static"
if _, err := os.Stat(staticDir); os.IsNotExist(err) {
log.Fatalf("Static directory %s does not exist", staticDir)
}

// API endpoints
http.HandleFunc("/api/aggregate", handleAggregate)
http.HandleFunc("/api/schema", handleSchema)
http.HandleFunc("/api/datasets", handleDatasets)
http.HandleFunc("/api/raw-data", handleRawData)

// Static files
http.Handle("/", http.FileServer(http.Dir(staticDir)))

port := ":8080"
fmt.Printf("Visual Aggregator Server starting on http://localhost%s\n", port)
fmt.Printf("Serving static files from: %s\n", staticDir)
fmt.Printf("Available datasets: %v\n", aggregator.GetAvailableDatasets())

log.Fatal(http.ListenAndServe(port, nil))
}

// handleAggregate handles POST /api/aggregate
func handleAggregate(w http.ResponseWriter, r *http.Request) {
if r.Method != http.MethodPost {
http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
return
}

body, err := io.ReadAll(r.Body)
if err != nil {
http.Error(w, fmt.Sprintf("Error reading body: %v", err), http.StatusBadRequest)
return
}
defer r.Body.Close()

var req aggregator.AggregateRequest
if err := json.Unmarshal(body, &req); err != nil {
http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
return
}

// Execute aggregation
result, err := aggregator.Aggregate(req)
if err != nil {
http.Error(w, fmt.Sprintf("Aggregation error: %v", err), http.StatusBadRequest)
return
}

w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(result)
}

// handleSchema handles GET /api/schema?dataset=name
func handleSchema(w http.ResponseWriter, r *http.Request) {
if r.Method != http.MethodGet {
http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
return
}

datasetName := r.URL.Query().Get("dataset")
if datasetName == "" {
http.Error(w, "Missing 'dataset' query parameter", http.StatusBadRequest)
return
}

data := aggregator.GetDataset(datasetName)
if data == nil {
http.Error(w, fmt.Sprintf("Dataset '%s' not found", datasetName), http.StatusNotFound)
return
}

schema := aggregator.DetectSchema(data)

w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(schema)
}

// handleDatasets handles GET /api/datasets
func handleDatasets(w http.ResponseWriter, r *http.Request) {
if r.Method != http.MethodGet {
http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
return
}

datasets := aggregator.GetAvailableDatasets()

w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(datasets)
}

// handleRawData handles GET /api/raw-data?dataset=name
func handleRawData(w http.ResponseWriter, r *http.Request) {
if r.Method != http.MethodGet {
http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
return
}

datasetName := r.URL.Query().Get("dataset")
if datasetName == "" {
http.Error(w, "Missing 'dataset' query parameter", http.StatusBadRequest)
return
}

data := aggregator.GetDataset(datasetName)
if data == nil {
http.Error(w, fmt.Sprintf("Dataset '%s' not found", datasetName), http.StatusNotFound)
return
}

// Pretty print JSON
jsonStr, err := aggregator.MarshalJSONWithIndent(data)
if err != nil {
http.Error(w, fmt.Sprintf("Error formatting JSON: %v", err), http.StatusInternalServerError)
return
}

w.Header().Set("Content-Type", "application/json")
w.Write([]byte(jsonStr))
}

// Helper function to get file path safely
func getFilePath(base, target string) (string, error) {
// Clean the target path to prevent directory traversal
target = filepath.Clean(target)

// Ensure target doesn't try to escape base directory
if strings.HasPrefix(target, "..") {
return "", fmt.Errorf("invalid path")
}

fullPath := filepath.Join(base, target)

// Verify the resolved path is still within base
absBase, err := filepath.Abs(base)
if err != nil {
return "", err
}

absFull, err := filepath.Abs(fullPath)
if err != nil {
return "", err
}

if !strings.HasPrefix(absFull, absBase) {
return "", fmt.Errorf("path escapes base directory")
}

return fullPath, nil
}
