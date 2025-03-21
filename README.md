## Authentication

Authenticating with an API token, for which the scope can be configured at the Cloudflare
dashboard.

Required authentication scopes:
- `Analytics:Read` is required for zone-level metrics


To authenticate this way, only set `CF_API_TOKEN`

## Configuration
The exporter can be configured using env variables or command flags.

| **KEY** | **description** |
|-|-|
| `CF_API_TOKEN` |  API authentication token |
| `CF_ZONES` |  (Optional) cloudflare zones to export, comma delimited list of zone ids. If not set, all zones from account are exported |
| `CF_EXCLUDE_ZONES` |  (Optional) cloudflare zones to exclude, comma delimited list of zone ids. If not set, no zones from account are excluded |
| `LISTEN` |  listen on addr:port (default `:8080`), omit addr to listen on all interfaces |
| `METRICS_PATH` |  path for metrics, default `/metrics` |
| `SCRAPE_DELAY` | scrape delay in seconds, default `300` |
| `CF_BATCH_SIZE` | cloudflare request zones batch size (1 - 10), default `10` |

Corresponding flags:
```
  -cf_api_token="": cloudflare api token
  -cf_zones="": cloudflare zones to export, comma delimited list
  -cf_exclude_zones="": cloudflare zones to exclude, comma delimited list
  -listen=":8080": listen on addr:port ( default :8080), omit addr to listen on all interfaces
  -metrics_path="/metrics": path for metrics, default /metrics
  -scrape_delay=300: scrape delay in seconds, defaults to 300
  -cf_batch_size=10: cloudflare zones batch size (1-10)
```

Note: `ZONE_<name>` configuration is not supported as flag.

## List of available metrics
```
# cloudflare_http_requests_adaptive HTTP requests grouped by host and status code (collected with adaptive sampling)

```

### Run
API token:
```
docker run --rm -p 8080:8080 -e CF_API_TOKEN=${CF_API_TOKEN} ghcr.io/lablabs/cloudflare_exporter
```

## License
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

See [LICENSE](LICENSE) for full details.

    Licensed to the Apache Software Foundation (ASF) under one
    or more contributor license agreements.  See the NOTICE file
    distributed with this work for additional information
    regarding copyright ownership.  The ASF licenses this file
    to you under the Apache License, Version 2.0 (the
    "License"); you may not use this file except in compliance
    with the License.  You may obtain a copy of the License at

      https://www.apache.org/licenses/LICENSE-2.0

    Unless required by applicable law or agreed to in writing,
    software distributed under the License is distributed on an
    "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
    KIND, either express or implied.  See the License for the
    specific language governing permissions and limitations
    under the License.

Originally authored by Labyrinth Labs - https://github.com/lablabs/cloudflare-exporter