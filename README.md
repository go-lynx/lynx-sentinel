# Sentinel Plugin for Lynx

Sentinel traffic protection plugin for Lynx. The current runtime scans YAML into `SentinelConfig`, loads flow, circuit-breaker, and system rules during startup, and can optionally start an in-process metrics collector plus a lightweight dashboard server.

## Current Scope

- Flow control, circuit breaker, and system rule loading from `lynx.sentinel`
- Convenience helpers such as `Entry`, `Execute`, `ProtectFunction`, HTTP middleware, and gRPC interceptors
- Optional in-process metrics collection and dashboard endpoints
- Programmatic rule add/remove helpers plus `ReloadRules()`
- Validation for app-name fallback, dashboard port, and basic rule boundaries

The following configuration items should currently be treated as compatibility or reserved fields rather than active runtime switches:

- Top-level `enabled`
- `log_level` as a per-plugin logger switch
- `data_source`
- `warm_up`
- `advanced`
- Legacy proto-only fields such as `default_qps_limit`, `enable_warm_up`, `warm_up_duration`, `default_circuit_breaker`, `enable_system_protection`, `enable_metrics`, `metrics_interval`, `enable_dashboard`, `dashboard_port`, `max_concurrent_requests`, `request_timeout`, and `enable_request_logging`

## Current YAML Model

Configure the plugin under `lynx.sentinel`:

```yaml
lynx:
  sentinel:
    app_name: "my-app"
    log_dir: "./logs/sentinel"
    log_level: "info"

    flow_rules:
      - resource: "/api/users"
        token_calculate_strategy: 0  # flow.Direct
        control_behavior: 0          # flow.Reject
        threshold: 100
        stat_interval_in_ms: 1000

    circuit_breaker_rules:
      - resource: "order-service"
        strategy: 1                  # circuitbreaker.ErrorRatio
        threshold: 0.5
        min_request_amount: 10
        retry_timeout_ms: 5000
        stat_interval_ms: 1000

    system_rules:
      - metric_type: 0               # system.Load
        trigger_count: 2.0

    metrics:
      enabled: true
      interval: "30s"

    dashboard:
      enabled: false
      port: 8719
```

## Effective Defaults And Validation

- `app_name` falls back to the Lynx application name, then to `lynx-app`
- `log_dir` defaults to `./logs/sentinel`
- `log_level` defaults to `info`, but the current runtime still follows global logging configuration instead of applying a per-plugin level
- `metrics.interval` defaults to `30s`
- `dashboard.port` defaults to `8719`
- `dashboard.port`, when set, must be within `1024` to `65535`
- Each `flow_rules[].resource` and `circuit_breaker_rules[].resource` must be non-empty
- Flow and circuit-breaker thresholds must be non-negative

## Capability Notes

- `CreateHTTPMiddleware()` protects HTTP requests using the extractor result or request path as the resource name
- `CreateGRPCInterceptor()` returns a middleware wrapper that exposes unary and stream interceptors
- `GetMetrics()`, `GetResourceStats()`, and `GetAllResourceStats()` require `metrics.enabled: true`
- `GetCircuitBreakerState()` currently returns a lightweight state view rather than full Sentinel internal breaker details
- The dashboard is a simple in-process HTTP server exposing `/api/metrics`, `/api/resources`, `/api/rules`, and `/api/health`; it is not the upstream Sentinel console

## Compatibility Notes

- `conf/sentinel.proto` is kept as a generated compatibility schema
- `README.md` and `conf/sentinel.yaml` describe the current runtime YAML model
- Fields listed in the reserved section above should not be documented as already wired runtime behavior until the framework-level config model is unified

## Validation And Examples

- Example file: `conf/sentinel.yaml`
- Validation baseline and reserved-field notes: `VALIDATION.md`
