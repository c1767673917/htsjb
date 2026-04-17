package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var Default = NewRegistry()

type Registry struct {
	httpMu        sync.Mutex
	httpCounts    map[string]uint64
	httpHistogram map[string][]uint64

	uploadsTotal      atomic.Uint64
	pdfRebuildsTotal  atomic.Uint64
	zipExportsTotal   atomic.Uint64
	rateLimitedTotal  atomic.Uint64
	sqliteErrorsTotal atomic.Uint64
}

var durationBuckets = []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

func NewRegistry() *Registry {
	return &Registry{
		httpCounts:    make(map[string]uint64),
		httpHistogram: make(map[string][]uint64),
	}
}

func (r *Registry) ObserveHTTPRequest(route, method string, status int, duration time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	key := fmt.Sprintf("route=%s,method=%s,status=%d", route, method, status)
	histKey := fmt.Sprintf("route=%s,method=%s", route, method)

	r.httpMu.Lock()
	defer r.httpMu.Unlock()
	r.httpCounts[key]++
	if _, ok := r.httpHistogram[histKey]; !ok {
		r.httpHistogram[histKey] = make([]uint64, len(durationBuckets)+1)
	}
	seconds := duration.Seconds()
	for idx, bucket := range durationBuckets {
		if seconds <= bucket {
			r.httpHistogram[histKey][idx]++
		}
	}
	r.httpHistogram[histKey][len(durationBuckets)]++
}

func (r *Registry) IncUploads()      { r.uploadsTotal.Add(1) }
func (r *Registry) IncPDFRebuilds()  { r.pdfRebuildsTotal.Add(1) }
func (r *Registry) IncZIPExports()   { r.zipExportsTotal.Add(1) }
func (r *Registry) IncRateLimited()  { r.rateLimitedTotal.Add(1) }
func (r *Registry) IncSQLiteErrors() { r.sqliteErrorsTotal.Add(1) }

func (r *Registry) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(r.render()))
	}
}

func (r *Registry) render() string {
	var builder strings.Builder
	builder.WriteString("# HELP app_http_requests_total Total HTTP requests.\n")
	builder.WriteString("# TYPE app_http_requests_total counter\n")

	r.httpMu.Lock()
	countKeys := make([]string, 0, len(r.httpCounts))
	for key := range r.httpCounts {
		countKeys = append(countKeys, key)
	}
	sort.Strings(countKeys)
	for _, key := range countKeys {
		builder.WriteString(fmt.Sprintf("app_http_requests_total{%s} %d\n", key, r.httpCounts[key]))
	}

	builder.WriteString("# HELP app_http_request_duration_seconds HTTP request duration histogram.\n")
	builder.WriteString("# TYPE app_http_request_duration_seconds histogram\n")
	histKeys := make([]string, 0, len(r.httpHistogram))
	for key := range r.httpHistogram {
		histKeys = append(histKeys, key)
	}
	sort.Strings(histKeys)
	for _, key := range histKeys {
		counts := r.httpHistogram[key]
		for idx, bucket := range durationBuckets {
			builder.WriteString(fmt.Sprintf("app_http_request_duration_seconds_bucket{%s,le=\"%.2f\"} %d\n", key, bucket, counts[idx]))
		}
		builder.WriteString(fmt.Sprintf("app_http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", key, counts[len(counts)-1]))
		builder.WriteString(fmt.Sprintf("app_http_request_duration_seconds_count{%s} %d\n", key, counts[len(counts)-1]))
	}
	r.httpMu.Unlock()

	writeSimpleCounter(&builder, "app_upload_submissions_total", "Total successful upload submissions.", r.uploadsTotal.Load())
	writeSimpleCounter(&builder, "app_pdf_rebuilds_total", "Total successful PDF rebuilds.", r.pdfRebuildsTotal.Load())
	writeSimpleCounter(&builder, "app_zip_exports_total", "Total ZIP exports.", r.zipExportsTotal.Load())
	writeSimpleCounter(&builder, "app_rate_limited_total", "Total rate-limited requests.", r.rateLimitedTotal.Load())
	writeSimpleCounter(&builder, "app_sqlite_errors_total", "Total SQLite operation errors observed by the app.", r.sqliteErrorsTotal.Load())
	return builder.String()
}

func writeSimpleCounter(builder *strings.Builder, name, help string, value uint64) {
	builder.WriteString(fmt.Sprintf("# HELP %s %s\n", name, help))
	builder.WriteString(fmt.Sprintf("# TYPE %s counter\n", name))
	builder.WriteString(fmt.Sprintf("%s %d\n", name, value))
}
