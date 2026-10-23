package telemetry

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// AllowedLabels is the only set of metric label keys (05 §2.1). A label that
// can take unbounded values (user, store or order id, phone, query text, IP)
// is never on it. TestRegisteredMetricsUseAllowedLabels fails on any metric
// that carries a key outside it.
var AllowedLabels = map[string]bool{
	"service": true, "route": true, "method": true, "status_class": true, "code": true,
	"app": true, "role": true, "kind": true, "class": true, "queue": true,
	"channel": true, "provider": true, "outcome": true, "feature": true, "model": true,
	"source": true, "city": true, "category": true, "lang": true, "platform": true,
	"conn": true, "app_version": true,
}

// JobClasses are the fixed job classes of 05 §2.1 and W1-11.
var JobClasses = []string{"timer", "money", "notify", "marketing", "ai", "rollup", "maintenance"}

// Other is the value every guarded label takes when its input is not known.
const Other = "other"

var (
	labelMu     sync.RWMutex
	labelValues = map[string]map[string]struct{}{
		"app":          set("web", "customer", "business", "admin", "site", "api", "worker", "other"),
		"platform":     set("web", "android", "ios", "desktop", "other"),
		"conn":         set("4g", "3g", "2g", "slow-2g", "wifi", "ethernet", "offline", "unknown", "other"),
		"method":       set("GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "other"),
		"status_class": set("1xx", "2xx", "3xx", "4xx", "5xx"),
		"class":        set(JobClasses...),
		"outcome":      set("ok", "error", "success", "failure", "retry", "cancelled", "discarded", "timeout", "other"),
		"role":         set("anonymous", "customer", "staff", "admin", "owner", "manager", "cashier", "other"),
		"service":      set("api", "worker", "other"),
		// kind, queue, channel, provider, feature, model, source, city, category
		// and lang get their values from RegisterLabelValues (W1-11 registers
		// job `kind` and `queue`; the domain that emits a metric registers the
		// rest).
	}
)

func set(vs ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(vs))
	for _, v := range vs {
		m[v] = struct{}{}
	}
	return m
}

// RegisterLabelValues lets an owner declare the values a label may take (W1-11
// registers job `kind` and `queue` values). It panics on a label that is not
// allowed, so a typo fails at start-up and in tests. Safe to call from init or
// boot, any number of times.
func RegisterLabelValues(label string, values ...string) {
	if !AllowedLabels[label] {
		panic(fmt.Sprintf("telemetry: label %q is not in the allow-list (05 §2.1)", label))
	}
	labelMu.Lock()
	defer labelMu.Unlock()
	m := labelValues[label]
	if m == nil {
		m = map[string]struct{}{}
		labelValues[label] = m
	}
	for _, v := range values {
		m[v] = struct{}{}
	}
}

// LabelValue returns v if it is a registered value of label, else "other".
// Use it for every label whose input is not a compile-time constant.
func LabelValue(label, v string) string {
	labelMu.RLock()
	defer labelMu.RUnlock()
	if _, ok := labelValues[label][v]; ok {
		return v
	}
	return Other
}

// RegisteredLabelValues returns the sorted values registered for label.
func RegisteredLabelValues(label string) []string {
	labelMu.RLock()
	defer labelMu.RUnlock()
	out := make([]string, 0, len(labelValues[label]))
	for v := range labelValues[label] {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

var reCode = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

// CodeLabel returns a problem code for the `code` label: only the stable
// snake_case form (codes are server-made, so this is bounded); "none" when the
// response was not a problem; anything else "other".
func CodeLabel(c string) string {
	if c == "" {
		return "none"
	}
	if reCode.MatchString(c) {
		return c
	}
	return Other
}

// StatusClass maps an HTTP status to "2xx" etc.
func StatusClass(status int) string {
	switch {
	case status >= 100 && status < 200:
		return "1xx"
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 300 && status < 400:
		return "3xx"
	case status >= 400 && status < 500:
		return "4xx"
	default:
		return "5xx"
	}
}

// CheckResourceMetrics returns one line per tz_ metric that breaks the 05
// §2.1 rules: a name without the tz_ prefix, or a data point carrying an
// attribute key outside AllowedLabels. Tests read a ManualReader and call it.
func CheckResourceMetrics(rm *metricdata.ResourceMetrics) []string {
	var bad []string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if !strings.HasPrefix(m.Name, MetricPrefix) {
				bad = append(bad, fmt.Sprintf("%s: name does not start with %s", m.Name, MetricPrefix))
			}
			for _, k := range metricKeys(m) {
				if !AllowedLabels[k] {
					bad = append(bad, fmt.Sprintf("%s: label %q is not in the allow-list", m.Name, k))
				}
			}
		}
	}
	return bad
}

func metricKeys(m metricdata.Metrics) []string {
	seen := map[string]struct{}{}
	add := func(set attribute.Set) {
		for _, kv := range set.ToSlice() {
			seen[string(kv.Key)] = struct{}{}
		}
	}
	switch d := m.Data.(type) {
	case metricdata.Sum[int64]:
		for _, p := range d.DataPoints {
			add(p.Attributes)
		}
	case metricdata.Sum[float64]:
		for _, p := range d.DataPoints {
			add(p.Attributes)
		}
	case metricdata.Gauge[int64]:
		for _, p := range d.DataPoints {
			add(p.Attributes)
		}
	case metricdata.Gauge[float64]:
		for _, p := range d.DataPoints {
			add(p.Attributes)
		}
	case metricdata.Histogram[int64]:
		for _, p := range d.DataPoints {
			add(p.Attributes)
		}
	case metricdata.Histogram[float64]:
		for _, p := range d.DataPoints {
			add(p.Attributes)
		}
	case metricdata.ExponentialHistogram[int64]:
		for _, p := range d.DataPoints {
			add(p.Attributes)
		}
	case metricdata.ExponentialHistogram[float64]:
		for _, p := range d.DataPoints {
			add(p.Attributes)
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
