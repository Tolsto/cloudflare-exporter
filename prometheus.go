//	Licensed to the Apache Software Foundation (ASF) under one
//	or more contributor license agreements.  See the NOTICE file
//	distributed with this work for additional information
//	regarding copyright ownership.  The ASF licenses this file
//	to you under the Apache License, Version 2.0 (the
//	"License"); you may not use this file except in compliance
//	with the License.  You may obtain a copy of the License at
//
//	https://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing,
//	software distributed under the License is distributed on an
//	"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
//	KIND, either express or implied.  See the License for the
//	specific language governing permissions and limitations
//	under the License.
//
// This file was originally authored by Labyrinth Labs - https://github.com/lablabs/cloudflare-exporter
// and has since been modified.

package main

import (
	"strconv"
	"sync"
	"time"

	cloudflare "github.com/cloudflare/cloudflare-go"
	prometheus "github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

type MetricName string

func (mn MetricName) String() string {
	return string(mn)
}

const (
	httpRequestsAdaptiveMetricName MetricName = "cloudflare_http_requests_adaptive"
)

type MetricsSet map[MetricName]struct{}

func (ms MetricsSet) Has(mn MetricName) bool {
	_, exists := ms[mn]
	return exists
}

func (ms MetricsSet) Add(mn MetricName) {
	ms[mn] = struct{}{}
}

// MetricID uniquely identifies a metric by name and label values
type MetricID string

// CloudflareCollector implements the prometheus.Collector interface
type CloudflareCollector struct {
	metrics         map[MetricID]prometheus.Metric
	metricFamilies  map[string]*prometheus.Desc
	metricsMutex    sync.Mutex
	lastCollectTime time.Time
}

// Create a new CloudflareCollector
func NewCloudflareCollector() *CloudflareCollector {
	c := &CloudflareCollector{
		metrics:         make(map[MetricID]prometheus.Metric),
		metricFamilies:  make(map[string]*prometheus.Desc),
		lastCollectTime: time.Now(),
	}

	// Register all metric families
	c.metricFamilies[httpRequestsAdaptiveMetricName.String()] = prometheus.NewDesc(
		httpRequestsAdaptiveMetricName.String(),
		"Cloudflare HTTP requests (collected via adaptive sampling)",
		[]string{"zone", "host", "edgeResponseCode", "originResponseCode"}, nil,
	)

	return c
}

// generateMetricID creates a unique identifier for a metric
func generateMetricID(metricName MetricName, labelValues ...string) MetricID {
	id := metricName.String()
	for _, label := range labelValues {
		id += ":" + label
	}
	return MetricID(id)
}

// Describe implements prometheus.Collector
func (c *CloudflareCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range c.metricFamilies {
		ch <- desc
	}
}

// Collect implements prometheus.Collector
func (c *CloudflareCollector) Collect(ch chan<- prometheus.Metric) {
	c.metricsMutex.Lock()
	defer c.metricsMutex.Unlock()

	// Send all metrics to the channel
	for _, metric := range c.metrics {
		ch <- metric
	}
}

// AddMetricWithTimestamp adds a new metric with timestamp to the collector
func (c *CloudflareCollector) AddMetricWithTimestamp(
	metricName MetricName,
	valueType prometheus.ValueType,
	value float64,
	timestamp time.Time,
	labelValues ...string) {

	metricNameStr := metricName.String()
	desc, ok := c.metricFamilies[metricNameStr]
	if !ok {
		log.Warnf("Unknown metric family: %s", metricNameStr)
		return
	}

	c.metricsMutex.Lock()
	defer c.metricsMutex.Unlock()

	// Create a new counter metric with timestamp
	metric := prometheus.NewMetricWithTimestamp(
		timestamp,
		prometheus.MustNewConstMetric(
			desc,
			valueType,
			value,
			labelValues...))

	// Generate a unique ID for this metric
	metricID := generateMetricID(metricName, labelValues...)

	// Store the metric, replacing any existing metric with same ID
	c.metrics[metricID] = metric
	c.lastCollectTime = time.Now()
}

// The global collector
var cfCollector = NewCloudflareCollector()

func registerCollector() {
	prometheus.MustRegister(cfCollector)
}

func collectHttpAdaptiveMetrics(zones []cloudflare.Zone, timestampForFetchCycle time.Time, wg *sync.WaitGroup) {
	defer wg.Done()

	zoneIDs := extractZoneIDs(filterNonFreePlanZones(zones))
	if len(zoneIDs) == 0 {
		return
	}

	r, timestamp, err := fetchHttpAdaptiveMetrics(zoneIDs, timestampForFetchCycle)
	if err != nil {
		return
	}

	for _, z := range r.Viewer.Zones {

		zoneName, _ := findZoneAccountName(zones, z.ZoneTag)
		for _, c := range z.Groups {
			cfCollector.AddMetricWithTimestamp(
				httpRequestsAdaptiveMetricName,
				prometheus.GaugeValue,
				float64(c.Count),
				timestamp,
				zoneName,
				c.Dimensions.Host,
				strconv.Itoa(int(c.Dimensions.EdgeResponseStatus)),
				strconv.Itoa(int(c.Dimensions.OriginResponseStatus)),
			)
		}
	}
}
