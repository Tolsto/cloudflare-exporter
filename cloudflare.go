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

type accountResp struct {
	WorkersInvocationsAdaptive []struct {
		Dimensions struct {
			ScriptName string `json:"scriptName"`
			Status     string `json:"status"`
		}

		Sum struct {
			Requests uint64  `json:"requests"`
			Errors   uint64  `json:"errors"`
			Duration float64 `json:"duration"`
		} `json:"sum"`

		Quantiles struct {
			CPUTimeP50   float32 `json:"cpuTimeP50"`
			CPUTimeP75   float32 `json:"cpuTimeP75"`
			CPUTimeP99   float32 `json:"cpuTimeP99"`
			CPUTimeP999  float32 `json:"cpuTimeP999"`
			DurationP50  float32 `json:"durationP50"`
			DurationP75  float32 `json:"durationP75"`
			DurationP99  float32 `json:"durationP99"`
			DurationP999 float32 `json:"durationP999"`
		} `json:"quantiles"`
	} `json:"workersInvocationsAdaptive"`
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

type zoneResp struct {
	HTTP1mGroups []struct {
		Dimensions struct {
			Datetime string `json:"datetime"`
		} `json:"dimensions"`
		Unique struct {
			Uniques uint64 `json:"uniques"`
		} `json:"uniq"`
		Sum struct {
			Bytes          uint64 `json:"bytes"`
			CachedBytes    uint64 `json:"cachedBytes"`
			CachedRequests uint64 `json:"cachedRequests"`
			Requests       uint64 `json:"requests"`
			BrowserMap     []struct {
				PageViews       uint64 `json:"pageViews"`
				UaBrowserFamily string `json:"uaBrowserFamily"`
			} `json:"browserMap"`
			ClientHTTPVersion []struct {
				Protocol string `json:"clientHTTPProtocol"`
				Requests uint64 `json:"requests"`
			} `json:"clientHTTPVersionMap"`
			ClientSSL []struct {
				Protocol string `json:"clientSSLProtocol"`
			} `json:"clientSSLMap"`
			ContentType []struct {
				Bytes                   uint64 `json:"bytes"`
				Requests                uint64 `json:"requests"`
				EdgeResponseContentType string `json:"edgeResponseContentTypeName"`
			} `json:"contentTypeMap"`
			Country []struct {
				Bytes             uint64 `json:"bytes"`
				ClientCountryName string `json:"clientCountryName"`
				Requests          uint64 `json:"requests"`
				Threats           uint64 `json:"threats"`
			} `json:"countryMap"`
			EncryptedBytes    uint64 `json:"encryptedBytes"`
			EncryptedRequests uint64 `json:"encryptedRequests"`
			IPClass           []struct {
				Type     string `json:"ipType"`
				Requests uint64 `json:"requests"`
			} `json:"ipClassMap"`
			PageViews      uint64 `json:"pageViews"`
			ResponseStatus []struct {
				EdgeResponseStatus int    `json:"edgeResponseStatus"`
				Requests           uint64 `json:"requests"`
			} `json:"responseStatusMap"`
			ThreatPathing []struct {
				Name     string `json:"threatPathingName"`
				Requests uint64 `json:"requests"`
			} `json:"threatPathingMap"`
			Threats uint64 `json:"threats"`
		} `json:"sum"`
	} `json:"httpRequests1mGroups"`

	FirewallEventsAdaptiveGroups []struct {
		Count      uint64 `json:"count"`
		Dimensions struct {
			Action                string `json:"action"`
			Source                string `json:"source"`
			RuleID                string `json:"ruleId"`
			ClientCountryName     string `json:"clientCountryName"`
			ClientRequestHTTPHost string `json:"clientRequestHTTPHost"`
		} `json:"dimensions"`
	} `json:"firewallEventsAdaptiveGroups"`

	HTTPRequestsAdaptiveGroups []struct {
		Count      uint64 `json:"count"`
		Dimensions struct {
			OriginResponseStatus  uint16 `json:"originResponseStatus"`
			ClientCountryName     string `json:"clientCountryName"`
			ClientRequestHTTPHost string `json:"clientRequestHTTPHost"`
		} `json:"dimensions"`
	} `json:"httpRequestsAdaptiveGroups"`

	HTTPRequestsEdgeCountryHost []struct {
		Count      uint64 `json:"count"`
		Dimensions struct {
			EdgeResponseStatus    uint16 `json:"edgeResponseStatus"`
			ClientCountryName     string `json:"clientCountryName"`
			ClientRequestHTTPHost string `json:"clientRequestHTTPHost"`
		} `json:"dimensions"`
	} `json:"httpRequestsEdgeCountryHost"`

	HealthCheckEventsAdaptiveGroups []struct {
		Count      uint64 `json:"count"`
		Dimensions struct {
			HealthStatus  string `json:"healthStatus"`
			OriginIP      string `json:"originIP"`
			FailureReason string `json:"failureReason"`
			Region        string `json:"region"`
			Fqdn          string `json:"fqdn"`
		} `json:"dimensions"`
	} `json:"healthCheckEventsAdaptiveGroups"`

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
