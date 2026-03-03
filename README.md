# taiko-runner

A high-performance load testing engine designed for standalone (CLI) or Kubernetes cluster-internal use. 

Supports HTTP load generation with a pluggable architecture for HTTP, gRPC and Kafka engines.

## Configuration

Configuration is loaded from a YAML file. The file location is resolved in priority order:

1. `--config-file` CLI flag
2. `CONFIG_FILE` environment variable
3. Default path: `/etc/taiko/config.yaml`

### Top-level parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `engine` | string | Engine type to use. Currently only `http` is supported. |
| `targets` | list | One or more target definitions (fields depend on engine type). |
| `load.duration` | duration | Total duration of the load test (e.g. `30s`, `5m`, `1h`). |
| `metrics.type` | string | Metrics output destination: `console`, `prometheus`, or `s3`. |
| `variables` | list | Dynamic variables for template substitution in URLs and bodies. |

### HTTP target fields (`engine: http`)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | yes | Target URL. Supports `{{variable}}` template substitution. |
| `method` | string | no | HTTP method (e.g. `GET`, `POST`). |
| `timeout` | duration | no | Per-request timeout (e.g. `5s`). |
| `body` | string | no | Request body. Supports `{{variable}}` template substitution. |
| `headers` | map | no | HTTP headers as key-value pairs. |
| `rps` | int | yes | Target requests per second for this endpoint. |
| `burst` | int | no | Token bucket burst size for the rate limiter (default: 1). |

### Variable types

Variables defined under `variables` are substituted into `{{name}}` placeholders in URLs and request bodies.

| Type | Description | Generator fields |
|------|-------------|-----------------|
| `uuid` | Random UUID v4 per request. | _(none)_ |
| `string_set` | Selects a value from a fixed list of strings. | `values` (list), `mode`: `seq` or `rnd` |
| `int_range` | Integer within a min/max range. | `min`, `max`, `mode`: `seq` or `rnd` |
| `int_set` | Selects an integer from a fixed list. | `values` (list), `mode`: `seq` or `rnd` |
| `timestamp` | Current Unix timestamp at request time. | _(none)_ |

### Example YAML configuration

```yaml
engine: http

targets:
  - url: http://localhost:8081/api/endpoint?id={{id}}
    method: POST
    timeout: 5s
    body: '{}'
    headers:
      Content-Type: application/json
    rps: 500

load:
  duration: 1m

metrics:
  type: console

variables:
  - name: id
    type: uuid
```
