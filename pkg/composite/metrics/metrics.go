/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type apiCallMetrics struct {
	latency *prometheus.HistogramVec
	errors  *prometheus.CounterVec
}

var (
	metricLabels = []string{
		"request", // API function that is begin invoked.
		"region",  // region (optional).
		"zone",    // zone (optional).
		"version", // API version.
	}

	apiMetrics = registerAPIMetrics(metricLabels...)
)

type metricContext struct {
	start time.Time
	// The cardinalities of attributes and metricLabels (defined above) must
	// match, or prometheus will panic.
	attributes []string
}

// Value for an unused label in the metric dimension.
const unusedMetricLabel = "<n/a>"

// Observe the result of a API call.
func (mc *metricContext) Observe(err error) error {
	apiMetrics.latency.WithLabelValues(mc.attributes...).Observe(
		time.Since(mc.start).Seconds())
	if err != nil {
		apiMetrics.errors.WithLabelValues(mc.attributes...).Inc()
	}

	return err
}

func NewMetricContext(prefix, request, region, zone, version string) *metricContext {
	if len(zone) == 0 {
		zone = unusedMetricLabel
	}
	if len(region) == 0 {
		region = unusedMetricLabel
	}
	return &metricContext{
		start:      time.Now(),
		attributes: []string{prefix + "_" + request, region, zone, version},
	}
}

// registerApiMetrics adds metrics definitions for a category of API calls.
func registerAPIMetrics(attributes ...string) *apiCallMetrics {
	metrics := &apiCallMetrics{
		latency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "gce_api_request_duration_seconds", // TODO: (shance) reconcile with cloudprovider
				Help:    "Latency of a GCE API call",
				Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 20, 40, 80, 160, 320},
			},
			attributes,
		),
		errors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gce_api_request_errors", // TODO: (shance) reconcile with cloudprovider
				Help: "Number of errors for an API call",
			},
			attributes,
		),
	}

	prometheus.MustRegister(metrics.latency)
	prometheus.MustRegister(metrics.errors)

	return metrics
}
