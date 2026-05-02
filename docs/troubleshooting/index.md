# Troubleshooting

Three documents:

1. [`cdt-codes.md`](cdt-codes.md) — the canonical reference for every
   `CDT0xxx` code emitted by `conduit doctor`. Bookmark this if
   you're paged on a `CDT0xxx` line; it has the fix.
2. [`common-issues.md`](common-issues.md) — symptom-driven guide.
   Start here if you don't know which CDT code applies but you know
   what's broken ("no data in Honeycomb", "agent won't start", "high
   memory").
3. This page — the index that maps **symptoms → CDT codes → fixes**
   and **CLI commands** you'll reach for first.

## First-response cheatsheet

```sh
# Did the parser accept the config?
sudo /usr/bin/conduit config --validate -c /etc/conduit/conduit.yaml

# Run all diagnostic checks (human-readable):
sudo /usr/bin/conduit doctor -c /etc/conduit/conduit.yaml

# Same, structured for piping into jq / a runbook:
sudo /usr/bin/conduit doctor --json -c /etc/conduit/conduit.yaml

# Just one family of checks:
sudo /usr/bin/conduit doctor --check output -c /etc/conduit/conduit.yaml

# Render the upstream collector YAML the agent would actually run:
sudo /usr/bin/conduit preview -c /etc/conduit/conduit.yaml | less

# Tail the agent's own logs (Linux/journald):
sudo journalctl -u conduit -n 100 --no-pager

# Tail the agent's own logs (Docker):
docker logs --tail 100 conduit

# Tail the agent's own logs (Kubernetes):
kubectl -n conduit logs -l app.kubernetes.io/name=conduit-agent --tail=100

# Tail the agent's own logs (Windows):
Get-EventLog -LogName Application -Source "Conduit" -Newest 50
```

Every doctor line carries a `CDT0xxx` code that links into
[`cdt-codes.md`](cdt-codes.md).

## Symptom → check → CDT code

| You're seeing… | Run this | Likely CDT |
|---|---|---|
| Service won't start, immediately exits with status 1 | `conduit config --validate` | [CDT0001](cdt-codes.md#cdt0001--config-syntax) |
| `validation error: …` in agent logs | `conduit doctor --check config` | [CDT0001](cdt-codes.md#cdt0001--config-syntax) |
| No data in Honeycomb, doctor reports auth failure | `conduit doctor --check output` | [CDT0102](cdt-codes.md#cdt0102--output-auth) |
| No data in Honeycomb, network not reachable | `conduit doctor --check output` | [CDT0101](cdt-codes.md#cdt0101--output-endpoint-reachable) |
| TLS verification errors in egress | `conduit doctor --check output` | [CDT0101](cdt-codes.md#cdt0101--output-endpoint-reachable) |
| Doctor warns about `insecure: true` | (intentional, set `insecure: false`) | [CDT0103](cdt-codes.md#cdt0103--output-tls-warning) |
| `bind: address already in use` on 4317 / 4318 | `conduit doctor --check receiver` | [CDT0201](cdt-codes.md#cdt0201--receiver-ports) |
| Filelog errors `permission denied` | `conduit doctor --check receiver` | [CDT0202](cdt-codes.md#cdt0202--receiver-permissions) |
| RED metric overflow series visible in Honeycomb | (validation rejects bad dims) | [CDT0501](cdt-codes.md#cdt0501--config-cardinality-warnings) |
| What collector core / build am I on? | `conduit doctor --check version` | [CDT0403](cdt-codes.md#cdt0403--version-compat) |

## Where to escalate

- **Reproducer fits in one `conduit.yaml`** → file an issue with the
  exact `conduit doctor --json` output and a redacted config.
- **Crashes, panics, FD leaks, OOM kills** → file an issue with the
  agent's log dump (`journalctl -u conduit --no-pager > log.txt`)
  and the host's `dmesg` if relevant.
- **CVE / signed-MSI / supply-chain question** →
  `security@conduit-obs.example` (or whatever channel the project
  ships once V0 is public — see
  [`docs/release/launch-checklist.md`](../release/launch-checklist.md)).

## Reading doctor JSON

Each result follows this shape:

```json
{
  "id": "CDT0101",
  "name": "output.endpoint_reachable",
  "severity": "fail",
  "summary": "TCP handshake to api.honeycomb.io:443 timed out after 5s",
  "details": "...",
  "docs_url": "https://github.com/conduit-obs/conduit-agent/blob/main/docs/troubleshooting/cdt-codes.md#cdt0101--output-endpoint-reachable"
}
```

Severity is one of `pass` / `skip` / `warn` / `fail`. The exit code
is non-zero only if at least one check returned `fail`. Warnings
exit zero on the assumption you've intentionally set the flag the
warning is about (`insecure: true`, an empty `overrides:`, etc.).

For the full schema and the symmetric Marshal/Unmarshal contract,
see [`internal/doctor/doctor.go`](../../internal/doctor/doctor.go).
