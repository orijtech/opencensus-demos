# appengine-memcache

A caching server for your APIs. Proxy requests through it to enable minimal
roundtrips outside of the network.

## Requirements
- Google Application credentials
- AWS credentials
- Optionally run Prometheus with the [prometheus.yml](./prometheus.yml) file provided

## Exporters used

Name|Access method
---|---
AWS X-Ray|https://console.aws.amazon.com/xray/home
Google Stackdriver Monitoring|
Google Stackdriver Tracing|https://console.cloud.google.com/traces/traces
Prometheus|Expose and visit port :9988 and route `/metrics` of your app
