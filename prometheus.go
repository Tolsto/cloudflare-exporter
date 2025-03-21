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
	"github.com/spf13/viper"
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

// TimestampedMetric holds a prometheus metric together with its timestamp
type TimestampedMetric struct {
	Timestamp time.Time
	Metric    prometheus.Metric
}

// CloudflareCollector implements the prometheus.Collector interface
type CloudflareCollector struct {
	metrics         []TimestampedMetric
	metricFamilies  map[string]*prometheus.Desc
	metricsMutex    sync.Mutex
	lastCollectTime time.Time
}

// Create a new CloudflareCollector
func NewCloudflareCollector() *CloudflareCollector {
	c := &CloudflareCollector{
		metrics:         []TimestampedMetric{},
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
	for _, tm := range c.metrics {
		ch <- tm.Metric
	}
}

// AddMetricWithTimestamp adds a new metric with timestamp to the collector
func (c *CloudflareCollector) AddMetricWithTimestamp(
	metricFamily string,
	valueType prometheus.ValueType,
	value float64,
	timestamp time.Time,
	labelValues ...string) {

	desc, ok := c.metricFamilies[metricFamily]
	if !ok {
		log.Warnf("Unknown metric family: %s", metricFamily)
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

	// Store in our timestamped structure
	c.metrics = append(c.metrics, TimestampedMetric{
		Timestamp: timestamp,
		Metric:    metric,
	})
	c.lastCollectTime = time.Now()
}

// ClearOldMetrics removes metrics older than the specified duration
func (c *CloudflareCollector) ClearOldMetrics(age time.Duration) {
	c.metricsMutex.Lock()
	defer c.metricsMutex.Unlock()

	cutoff := time.Now().Add(-age)
	filteredMetrics := []TimestampedMetric{}

	// Keep only metrics newer than the cutoff
	for _, tm := range c.metrics {
		if tm.Timestamp.After(cutoff) {
			filteredMetrics = append(filteredMetrics, tm)
		}
	}

	// Replace with filtered metrics
	log.Infof("Cleared %d old metrics (keeping %d)", len(c.metrics)-len(filteredMetrics), len(filteredMetrics))
	c.metrics = filteredMetrics
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
				httpRequestsAdaptiveMetricName.String(),
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

	scrapeDelay := time.Duration(viper.GetInt("scrape_delay")) * time.Second
	// Clean up metrics older than 5 minutes after adding the new ones
	cfCollector.ClearOldMetrics(5*time.Minute + scrapeDelay)
}
