# Grafana Cloud dashboards

Source-of-truth definitions for the production dashboards on the
`olivesofa1456` Grafana Cloud stack. Telemetry arrives over the OTLP
gateway (see `docs` in the observability setup: go-api direct, cpp
accelerator via the otel-collector sidecar on the Jetson).

| File | Dashboard | UID |
|---|---|---|
| `prod-logs.json` | Learning CUDA — Production Logs | `cuda-prod-logs` |
| `prod-metrics.json` | Learning CUDA — Production Metrics | `cuda-prod-metrics` |

Both use the stack-provisioned datasources (`grafanacloud-logs`,
`grafanacloud-prom`), so they work on any Grafana Cloud stack with those
default UIDs.

## Applying changes

Edit the JSON here, then push to the stack (token: service account in
`.secrets/grafana.env`, `TOKEN`):

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -X POST https://olivesofa1456.grafana.net/api/dashboards/db \
     -d "{\"dashboard\": $(cat prod-logs.json), \"overwrite\": true}"
```

The `overwrite` + stable `uid` pair updates in place instead of creating
duplicates. These definitions replace the former self-hosted Grafana
files (`config/grafana-dashboard-logs.json` etc.), which were removed
when production telemetry moved to Grafana Cloud.
