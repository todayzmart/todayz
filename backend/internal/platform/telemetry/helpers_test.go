package telemetry

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func metricOpt(kv ...attribute.KeyValue) metric.AddOption { return metric.WithAttributes(kv...) }
