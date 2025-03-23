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
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nelkinda/health-go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cloudflare "github.com/cloudflare/cloudflare-go"
	log "github.com/sirupsen/logrus"
)

func getTargetZones() []string {
	var zoneIDs []string

	if len(viper.GetString("cf_zones")) > 0 {
		zoneIDs = strings.Split(viper.GetString("cf_zones"), ",")
	} else {
		// deprecated
		for _, e := range os.Environ() {
			if strings.HasPrefix(e, "ZONE_") {
				split := strings.SplitN(e, "=", 2)
				zoneIDs = append(zoneIDs, split[1])
			}
		}
	}
	return zoneIDs
}

func getExcludedZones() []string {
	var zoneIDs []string

	if len(viper.GetString("cf_exclude_zones")) > 0 {
		zoneIDs = strings.Split(viper.GetString("cf_exclude_zones"), ",")
	}
	return zoneIDs
}

func filterZones(all []cloudflare.Zone, target []string) []cloudflare.Zone {
	var filtered []cloudflare.Zone

	if (len(target)) == 0 {
		return all
	}

	for _, tz := range target {
		for _, z := range all {
			if tz == z.ID {
				filtered = append(filtered, z)
				log.Info("Filtering zone: ", z.ID, " ", z.Name)
			}
		}
	}

	return filtered
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func filterExcludedZones(all []cloudflare.Zone, exclude []string) []cloudflare.Zone {
	var filtered []cloudflare.Zone

	if (len(exclude)) == 0 {
		return all
	}

	for _, z := range all {
		if contains(exclude, z.ID) {
			log.Info("Exclude zone: ", z.ID, " ", z.Name)
		} else {
			filtered = append(filtered, z)
		}
	}

	return filtered
}

func fetchMetrics(timestampForFetchCycle time.Time) {
	var wg sync.WaitGroup

	zones := fetchZones()
	if len(zones) == 0 {
		log.Warn("No zones retrieved, will retry on next scrape")
		return
	}

	filteredZones := filterExcludedZones(filterZones(zones, getTargetZones()), getExcludedZones())

	// Make requests in groups of cfgBatchSize to avoid rate limit
	// 10 is the maximum amount of zones you can request at once
	for len(filteredZones) > 0 {
		sliceLength := viper.GetInt("cf_batch_size")
		if len(filteredZones) < viper.GetInt("cf_batch_size") {
			sliceLength = len(filteredZones)
		}

		targetZones := filteredZones[:sliceLength]
		filteredZones = filteredZones[len(targetZones):]

		wg.Add(1)
		go collectHttpAdaptiveMetrics(targetZones, timestampForFetchCycle, &wg)
	}

	wg.Wait()
	log.Debug("Metric fetch cycle completed")
}

func runExporter() {
	cfgMetricsPath := viper.GetString("metrics_path")

	if !(len(viper.GetString("cf_api_token")) > 0) {
		log.Fatal("Please provide CF_API_TOKEN")
	}
	if viper.GetInt("cf_batch_size") < 1 || viper.GetInt("cf_batch_size") > 10 {
		log.Fatal("CF_BATCH_SIZE must be between 1 and 10")
	}
	customFormatter := new(log.TextFormatter)
	customFormatter.TimestampFormat = "2006-01-02 15:04:05"
	log.SetFormatter(customFormatter)
	customFormatter.FullTimestamp = true

	registerCollector()

	go func() {
		for ; true; <-time.NewTicker(60 * time.Second).C {
			timestampForFetchCycle := time.Now()
			go fetchMetrics(timestampForFetchCycle)
		}
	}()

	// This section will start the HTTP server and expose
	// any metrics on the /metrics endpoint.
	if !strings.HasPrefix(viper.GetString("metrics_path"), "/") {
		cfgMetricsPath = "/" + viper.GetString("metrics_path")
	}

	http.Handle(cfgMetricsPath, promhttp.Handler())
	h := health.New(health.Health{})
	http.HandleFunc("/health", h.Handler)

	log.Info("Beginning to serve metrics on ", viper.GetString("listen"), cfgMetricsPath)

	server := &http.Server{
		Addr:              viper.GetString("listen"),
		ReadHeaderTimeout: 3 * time.Second,
	}

	log.Fatal(server.ListenAndServe())
}

func main() {
	var cmd = &cobra.Command{
		Use:   "viper-test",
		Short: "testing viper",
		Run: func(_ *cobra.Command, _ []string) {
			runExporter()
		},
	}

	//vip := viper.New()
	viper.AutomaticEnv()

	flags := cmd.Flags()

	flags.String("listen", ":8080", "listen on addr:port ( default :8080), omit addr to listen on all interfaces")
	viper.BindEnv("listen")
	viper.SetDefault("listen", ":8080")

	flags.String("metrics_path", "/metrics", "path for metrics, default /metrics")
	viper.BindEnv("metrics_path")
	viper.SetDefault("metrics_path", "/metrics")

	flags.String("cf_api_token", "", "cloudflare api token")
	viper.BindEnv("cf_api_token")

	flags.String("cf_zones", "", "cloudflare zones to export, comma delimited list")
	viper.BindEnv("cf_zones")
	viper.SetDefault("cf_zones", "")

	flags.String("cf_exclude_zones", "", "cloudflare zones to exclude, comma delimited list")
	viper.BindEnv("cf_exclude_zones")
	viper.SetDefault("cf_exclude_zones", "")

	flags.Int("scrape_delay", 300, "scrape delay in seconds, defaults to 300")
	viper.BindEnv("scrape_delay")
	viper.SetDefault("scrape_delay", 300)

	flags.Int("cf_batch_size", 10, "cloudflare zones batch size (1-10), defaults to 10")
	viper.BindEnv("cf_batch_size")
	viper.SetDefault("cf_batch_size", 10)

	viper.BindPFlags(flags)
	cmd.Execute()
}
