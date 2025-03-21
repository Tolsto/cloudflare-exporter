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
	"context"
	"strings"
	"time"

	cloudflare "github.com/cloudflare/cloudflare-go"
	"github.com/machinebox/graphql"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var (
	cfGraphQLEndpoint = "https://api.cloudflare.com/client/v4/graphql/"
	maxTries          = 3 // Total number of attempts (1 initial + 2 retries)
)

type cloudflareResponseHttpAdaptive struct {
	Viewer struct {
		Zones []zoneRespHttpAdaptive `json:"zones"`
	} `json:"viewer"`
}

type zoneRespHttpAdaptive struct {
	Groups []struct {
		Dimensions struct {
			Host                 string `json:"clientRequestHTTPHost"`
			EdgeResponseStatus   uint16 `json:"edgeResponseStatus"`
			OriginResponseStatus uint16 `json:"originResponseStatus"`
		} `json:"dimensions"`
		Count uint64 `json:"count"`
		Sum   struct {
			EdgeResponseBytes uint64 `json:"edgeResponseBytes"`
		} `json:"sum"`
		Avg struct {
			SampleInterval           float64 `json:"sampleInterval"`
			OriginResponseDurationMs float64 `json:"originResponseDurationMs"`
		} `json:"avg"`
	} `json:"httpRequestsAdaptiveGroups"`

	ZoneTag string `json:"zoneTag"`
}

// retryOperation executes the given function with retries
func retryOperation(operationName string, operation func() error) error {
	var err error
	for attempt := 1; attempt <= maxTries; attempt++ {
		err = operation()
		if err == nil {
			return nil
		}

		// Log the error but continue with retry if not the last attempt
		if attempt < maxTries {
			log.Warnf("%s failed (attempt %d/%d): %v - retrying in %d seconds",
				operationName, attempt, maxTries, err, attempt)
			time.Sleep(1 * time.Second)
			continue
		}
	}
	return err
}

func fetchZones() []cloudflare.Zone {
	var zones []cloudflare.Zone

	err := retryOperation("Fetch zones", func() error {
		var api *cloudflare.API
		var err error
		api, err = cloudflare.NewWithAPIToken(viper.GetString("cf_api_token"))
		if err != nil {
			return err
		}

		ctx := context.Background()
		z, err := api.ListZones(ctx)
		if err != nil {
			return err
		}

		zones = z
		return nil
	})

	if err != nil {
		log.Error("Failed to fetch zones after all attempts: ", err)
		return []cloudflare.Zone{}
	}

	return zones
}

func getMetricsTimestamp(timestampForFetchCycle time.Time) time.Time {
	now := timestampForFetchCycle.Add(-time.Duration(viper.GetInt("scrape_delay")) * time.Second).UTC()
	s := 60 * time.Second
	return now.Truncate(s)
}

func fetchHttpAdaptiveMetrics(zoneIDs []string, timestampForFetchCycle time.Time) (*cloudflareResponseHttpAdaptive, time.Time, error) {
	var resp cloudflareResponseHttpAdaptive
	var maxTime time.Time

	err := retryOperation("Fetch http request analytics", func() error {
		maxTime = getMetricsTimestamp(timestampForFetchCycle)
		maxTimeMinus1m := maxTime.Add(-60 * time.Second)

		request := graphql.NewRequest(`
		query ($zoneIDs: [String!], $mintime: Time!, $maxtime: Time!, $limit: Int!) {
			viewer {
				zones(filter: { zoneTag_in: $zoneIDs }) {
					zoneTag
					httpRequestsAdaptiveGroups(
						limit: $limit
						filter: { datetime_geq: $mintime, datetime_lt: $maxtime }
					) {
						count
						avg {
							sampleInterval
							originResponseDurationMs
						}
						dimensions {
							clientRequestHTTPHost
							originResponseStatus
							edgeResponseStatus
						}
						sum {
							edgeResponseBytes
						}
					}
				}
			}
		}
	`)
		request.Header.Set("Authorization", "Bearer "+viper.GetString("cf_api_token"))
		request.Var("limit", 9999)
		request.Var("maxtime", maxTime)
		request.Var("mintime", maxTimeMinus1m)
		request.Var("zoneIDs", zoneIDs)

		ctx := context.Background()
		graphqlClient := graphql.NewClient(cfGraphQLEndpoint)
		if err := graphqlClient.Run(ctx, request, &resp); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		log.Error("Failed to http request totals after all attempts: ", err)
		return nil, maxTime, err
	}

	return &resp, maxTime, nil
}

func findZoneAccountName(zones []cloudflare.Zone, ID string) (string, string) {
	for _, z := range zones {
		if z.ID == ID {
			return z.Name, strings.ToLower(strings.ReplaceAll(z.Account.Name, " ", "-"))
		}
	}

	return "", ""
}

func extractZoneIDs(zones []cloudflare.Zone) []string {
	var IDs []string

	for _, z := range zones {
		IDs = append(IDs, z.ID)
	}

	return IDs
}

func filterNonFreePlanZones(zones []cloudflare.Zone) (filteredZones []cloudflare.Zone) {
	for _, z := range zones {
		if z.Plan.ZonePlanCommon.ID != "0feeeeeeeeeeeeeeeeeeeeeeeeeeeeee" {
			filteredZones = append(filteredZones, z)
		}
	}
	return
}
