package metrics

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// ContentType is the Prometheus text exposition content type.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

// Write emits the registry in Prometheus text exposition format 0.0.4 with
// deterministic ordering and no timestamps.
func (r *Registry) Write(ctx context.Context, w io.Writer) error {
	var b strings.Builder

	// Build info and process gauges.
	writeHeader(&b, "vandyke_build_info", "Build version and platform of the running binary.", "gauge")
	writeSample(&b, "vandyke_build_info", []label{
		{"version", r.version},
		{"go_version", goVersion()},
		{"goos", runtime.GOOS},
		{"goarch", runtime.GOARCH},
	}, "1")

	writeHeader(&b, "vandyke_process_start_time_seconds", "Unix timestamp of when the process started.", "gauge")
	writeSample(&b, "vandyke_process_start_time_seconds", nil,
		fmt.Sprintf("%.3f", float64(r.start.UnixNano())/1e9))

	writeHeader(&b, "vandyke_process_uptime_seconds", "Seconds since the process started.", "gauge")
	writeSample(&b, "vandyke_process_uptime_seconds", nil, formatFloat(r.uptime()))

	if size, ok := r.dbSize(); ok {
		writeHeader(&b, "vandyke_db_size_bytes", "Size of the main SQLite database file, excluding -wal and -shm.", "gauge")
		writeSample(&b, "vandyke_db_size_bytes", nil, strconv.FormatInt(size, 10))
	}

	// DB-backed gauges. A failure drops the series but keeps the scrape
	// usable, and increments no counter.
	dbUp := false
	if r.snapshot != nil {
		if snap, err := r.snapshot(ctx); err == nil {
			dbUp = true
			writeHeader(&b, "vandyke_entries_total", "Entries in the ledger, including trashed rows.", "gauge")
			writeSample(&b, "vandyke_entries_total", nil, strconv.Itoa(snap.Total))
			writeHeader(&b, "vandyke_entries_live", "Entries not in the trash.", "gauge")
			writeSample(&b, "vandyke_entries_live", nil, strconv.Itoa(snap.Live))
			writeHeader(&b, "vandyke_entries_trashed", "Entries currently in the trash.", "gauge")
			writeSample(&b, "vandyke_entries_trashed", nil, strconv.Itoa(snap.Trashed))
			writeHeader(&b, "vandyke_entries_with_photo", "Live entries with a photo.", "gauge")
			writeSample(&b, "vandyke_entries_with_photo", nil, strconv.Itoa(snap.WithPhoto))
			writeHeader(&b, "vandyke_webhook_pending_deliveries", "Webhook deliveries waiting for an attempt or retry.", "gauge")
			writeSample(&b, "vandyke_webhook_pending_deliveries", nil, strconv.Itoa(snap.PendingDeliveries))
		} else {
			r.logger.Warn("metrics database query failed", "error", err)
		}
	}
	writeHeader(&b, "vandyke_db_up", "Whether the scrape-time database query succeeded (1) or failed (0).", "gauge")
	if dbUp {
		writeSample(&b, "vandyke_db_up", nil, "1")
	} else {
		writeSample(&b, "vandyke_db_up", nil, "0")
	}

	r.mu.Lock()
	queueDepth := 0
	if r.queueDepth != nil {
		queueDepth = r.queueDepth()
	}
	r.mu.Unlock()
	writeHeader(&b, "vandyke_webhook_queue_depth", "Events currently waiting in the in-process webhook queue.", "gauge")
	writeSample(&b, "vandyke_webhook_queue_depth", nil, strconv.Itoa(queueDepth))

	writeHeader(&b, "vandyke_webhook_dropped_total", "Events dropped because the webhook queue was full.", "counter")
	writeSample(&b, "vandyke_webhook_dropped_total", nil, strconv.FormatInt(r.dropped.Load(), 10))

	writeHeader(&b, "vandyke_webhook_deliveries_total", "Terminal webhook delivery outcomes.", "counter")
	for i, result := range deliveryResults {
		writeSample(&b, "vandyke_webhook_deliveries_total", []label{{"result", result}},
			strconv.FormatInt(r.deliveries[i].Load(), 10))
	}

	writeHeader(&b, "vandyke_webhook_attempts_total", "Webhook delivery attempts by result.", "counter")
	for i, result := range attemptResults {
		writeSample(&b, "vandyke_webhook_attempts_total", []label{{"result", result}},
			strconv.FormatInt(r.attempts[i].Load(), 10))
	}

	writeHeader(&b, "vandyke_entry_additions_total", "Entry additions by outcome.", "counter")
	for i, outcome := range additionOutcomes {
		writeSample(&b, "vandyke_entry_additions_total", []label{{"outcome", outcome}},
			strconv.FormatInt(r.additions[i].Load(), 10))
	}

	writeHeader(&b, "vandyke_rate_limited_requests_total", "Entry additions denied by a rate limiter.", "counter")
	writeSample(&b, "vandyke_rate_limited_requests_total", nil, strconv.FormatInt(r.rateLimited.Load(), 10))

	// HTTP request counters, sorted by method, route and status.
	r.mu.Lock()
	requestKeys := make([]requestKey, 0, len(r.requests))
	for key := range r.requests {
		requestKeys = append(requestKeys, key)
	}
	sort.Slice(requestKeys, func(i, j int) bool {
		a, b := requestKeys[i], requestKeys[j]
		if a.method != b.method {
			return a.method < b.method
		}
		if a.route != b.route {
			return a.route < b.route
		}
		return a.status < b.status
	})
	writeHeader(&b, "vandyke_http_requests_total", "HTTP requests by method, mux route pattern and status.", "counter")
	for _, key := range requestKeys {
		writeSample(&b, "vandyke_http_requests_total", []label{
			{"method", key.method},
			{"route", key.route},
			{"status", key.status},
		}, strconv.FormatInt(r.requests[key].Load(), 10))
	}

	durationKeys := make([]durationKey, 0, len(r.durations))
	for key := range r.durations {
		durationKeys = append(durationKeys, key)
	}
	sort.Slice(durationKeys, func(i, j int) bool {
		a, b := durationKeys[i], durationKeys[j]
		if a.method != b.method {
			return a.method < b.method
		}
		return a.route < b.route
	})
	writeHeader(&b, "vandyke_http_request_duration_seconds", "HTTP request duration by method and mux route pattern.", "histogram")
	for _, key := range durationKeys {
		hist := r.durations[key]
		labels := []label{{"method", key.method}, {"route", key.route}}
		for i, bound := range durationBuckets {
			writeSample(&b, "vandyke_http_request_duration_seconds_bucket",
				append(append([]label{}, labels...), label{"le", formatFloat(bound)}),
				strconv.FormatUint(hist.buckets[i], 10))
		}
		writeSample(&b, "vandyke_http_request_duration_seconds_bucket",
			append(append([]label{}, labels...), label{"le", "+Inf"}),
			strconv.FormatUint(hist.inf, 10))
		writeSample(&b, "vandyke_http_request_duration_seconds_sum", labels, formatFloat(hist.sum))
		writeSample(&b, "vandyke_http_request_duration_seconds_count", labels,
			strconv.FormatUint(hist.count, 10))
	}
	r.mu.Unlock()

	_, err := io.WriteString(w, b.String())
	return err
}

type label struct {
	name  string
	value string
}

func writeHeader(b *strings.Builder, name, help, typ string) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s %s\n", name, typ)
}

func writeSample(b *strings.Builder, name string, labels []label, value string) {
	b.WriteString(name)
	if len(labels) > 0 {
		b.WriteByte('{')
		for i, l := range labels {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(l.name)
			b.WriteString(`="`)
			b.WriteString(escapeLabel(l.value))
			b.WriteByte('"')
		}
		b.WriteByte('}')
	}
	b.WriteByte(' ')
	b.WriteString(value)
	b.WriteByte('\n')
}

func escapeLabel(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return replacer.Replace(value)
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func statusString(status int) string {
	return strconv.Itoa(status)
}
