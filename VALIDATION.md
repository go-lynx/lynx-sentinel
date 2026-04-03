# Validation

## Automated Baseline

Run from the module root:

```bash
GOWORK=off go test ./...
```

## Enforced Validation

The current runtime validates only the fields that are actually wired into startup and rule loading:

- `app_name` must be non-empty after fallback resolution
- `dashboard.port`, when set, must stay within `1024` to `65535`
- `flow_rules[].resource` must be non-empty
- `flow_rules[].threshold` must be non-negative
- `circuit_breaker_rules[].resource` must be non-empty
- `circuit_breaker_rules[].threshold` must be non-negative

## Effective Runtime Defaults

- `app_name`: current Lynx app name, then `lynx-app`
- `log_dir`: `./logs/sentinel`
- `log_level`: `info`
- `metrics.interval`: `30s`
- `dashboard.port`: `8719`

## Compatibility And Reserved Fields

These items exist in the schema or config struct, but they are not fully wired runtime controls in the current module:

- Top-level `enabled`
- `log_level` as a per-plugin logger switch
- `data_source`
- `warm_up`
- `advanced`
- Proto-only compatibility fields:
  `default_qps_limit`, `enable_warm_up`, `warm_up_duration`, `default_circuit_breaker`,
  `enable_system_protection`, `enable_metrics`, `metrics_interval`, `enable_dashboard`,
  `dashboard_port`, `max_concurrent_requests`, `request_timeout`, `enable_request_logging`

## Recommended Manual Checks

- Verify one flow rule, one circuit-breaker rule, and one system rule against a running application
- If `metrics.enabled` is `true`, verify `GetMetrics()` and dashboard endpoints return non-empty data
- If HTTP or gRPC interception is used, verify the resource extractor maps requests to stable resource names
