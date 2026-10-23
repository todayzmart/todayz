package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	ptel "github.com/todayz/backend/internal/platform/telemetry"
)

// rawSampleEvery keeps 1 RUM sample in 100 as a raw log line for drill-down (05 §2.5).
const rawSampleEvery = 100

type rumSample struct {
	Type   string         `json:"type"`
	Name   string         `json:"name"`
	Value  float64        `json:"value"`
	Route  string         `json:"route"`
	Detail map[string]any `json:"detail"`
	T      int64          `json:"t"`
}

type rumBatch struct {
	App      string      `json:"app"`
	Release  string      `json:"release"`
	Platform string      `json:"platform"`
	Conn     string      `json:"conn"`
	Samples  []rumSample `json:"samples"`
}

// vitalMetrics maps the client's metric names to the RUM series.
var vitalMetrics = map[string]string{
	"LCP": "lcp", "INP": "inp", "CLS": "cls", "FCP": "fcp", "TTFB": "ttfb",
	"route_change": "route_change", "long_task": "long_task",
}

var reDigits = regexp.MustCompile(`\d+`)

// rumRoute keeps what looks like a route template; anything else is "unknown". The metric layer
// caps how many distinct routes an app can create, this only refuses garbage early.
func rumRoute(route string) string {
	if !strings.HasPrefix(route, "/") || strings.ContainsAny(route, "?# \t\n") {
		return "unknown"
	}
	return route
}

// errorFingerprint groups the same error across users: app, error name and the redacted message
// with digits flattened (so a changing count or id does not split a group).
func errorFingerprint(app, name, message string) string {
	h := sha256.Sum256([]byte(app + "|" + name + "|" + reDigits.ReplaceAllString(message, "0")))
	return hex.EncodeToString(h[:6])
}

func (s *Service) postRum(w http.ResponseWriter, r *http.Request) {
	var b rumBatch
	if !decode(w, r, &b) {
		return
	}
	ctx := r.Context()
	log := ptel.Logger(ctx)
	version := b.Release
	if i := strings.LastIndex(version, "@"); i >= 0 {
		version = version[i+1:]
	}
	for _, sm := range b.Samples {
		route := rumRoute(sm.Route)
		switch sm.Type {
		case "vital":
			metric, ok := vitalMetrics[sm.Name]
			if !ok {
				continue
			}
			s.d.Metrics.RecordRUM(ctx, ptel.RUMSample{
				App: b.App, Route: route, Platform: b.Platform, Conn: b.Conn,
				AppVersion: version, Metric: metric, Value: sm.Value,
			})
			if s.samples.Add(1)%rawSampleEvery == 0 {
				log.InfoContext(ctx, "rum sample",
					slog.String("source", "rum"), slog.String("app", b.App), slog.String("app_version", version),
					slog.String("route", route), slog.String("metric", metric), slog.Float64("value", sm.Value))
			}
		case "error":
			s.d.Metrics.RecordRUM(ctx, ptel.RUMSample{
				App: b.App, Route: route, Platform: b.Platform, Conn: b.Conn,
				AppVersion: version, Metric: "error",
			})
			msg, _ := sm.Detail["message"].(string)
			// The client scrubs before sending; the server does not trust it.
			msg = ptel.RedactString(truncate(msg, 300))
			name := ptel.RedactString(truncate(sm.Name, 100))
			log.ErrorContext(ctx, "client error",
				slog.String("source", "rum"), slog.String("app", b.App), slog.String("app_version", version),
				slog.String("route", route), slog.String("err_name", name), slog.String("err_message", msg),
				slog.String("fingerprint", errorFingerprint(b.App, name, msg)))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
