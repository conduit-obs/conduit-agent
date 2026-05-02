# Conduit V0 — demo script

A 30-minute walkthrough that takes a brand-new viewer from "what is
this" to "data is in Honeycomb, I see how to operate it". Designed to
be **rehearsable in front of strangers** — every command lands, every
slide has a fallback, and the worst-case "the WiFi died" path is
called out.

> **Length**: 30 minutes hard cap, 25 minutes target.
> **Audience**: platform engineers, SREs, observability practitioners
> who have shipped *some* OTel before.
> **Goal**: leave them able to imagine running this on their own
> fleet, and confident the project is real (not a demo-ware prototype).

## Prep checklist (T-30 minutes)

- [ ] Fresh Linux VM ready and reachable (Ubuntu 22.04, e.g. EC2
      `t3.small`). SSH in and confirm `sudo` works.
- [ ] A Honeycomb sandbox env — confirm the API key is in your
      `~/.zshenv` as `HONEYCOMB_API_KEY`. Open the env in the browser,
      filter datasets to `last hour`, and **delete any leftover
      `demo-*` datasets** so the new ones light up cleanly mid-demo.
- [ ] Local checkout of `conduit-agent` on `main`, with the latest
      release tag pulled. `make build` once so the binary is warm if
      you need to drop into local mode.
- [ ] Docker desktop running, in case the VM connection dies. The
      `deploy/docker/compose-linux-host.yaml` recipe is your fallback.
- [ ] Browser tabs queued in this order:
      1. The conduit-agent GitHub repo (README visible).
      2. Honeycomb → environment → Datasets list.
      3. `docs/getting-started/linux.md` (in case you need to copy a
         command).
      4. The Honeycomb UI's "create new dashboard" page.
- [ ] Terminal: split into 3 panes — local repo, SSH session, scratch
      pane for `conduit doctor` runs.

## Slide deck (4 slides only — keep it small)

1. **What Conduit is** (45 seconds): "vendor-agnostic OTel Collector
   distribution; one small `conduit.yaml` instead of a 500-line
   `otelcol-config.yaml`."
2. **The output picture** (45 seconds): the three modes
   (`honeycomb` / `otlp` / `gateway`), the same agent for all of them.
3. **What we're going to do in the next 25 minutes** (30 seconds):
   "install on a Linux box, watch data appear in Honeycomb, run
   `conduit doctor`, switch output mode, see what `overrides:` is for."
4. **Where to follow up** (30 seconds, end of demo): repo URL,
   getting-started links, docs site.

The rest is live.

---

## Act 1 — install in 60 seconds (T+1m → T+3m)

**Setup** (you've SSH'd into the VM already):

```sh
# Show that there's nothing here yet:
which conduit       # not found
ls /etc/conduit/    # not found
```

**Install**:

```sh
curl -fsSL https://raw.githubusercontent.com/conduit-obs/conduit-agent/main/scripts/install_linux.sh \
  | sudo bash -s -- \
    --api-key="$HONEYCOMB_API_KEY" \
    --service-name=demo-edge \
    --deployment-environment=demo
```

**Talking points while it runs** (~30 seconds):

- "This is one binary, no external dependencies. The script picks the
  right `.deb` for Ubuntu, creates the system user, writes the env
  file, and starts the systemd unit."
- "Re-running it is a no-op — `apt-get install` is upgrade-in-place."

**Verify**:

```sh
sudo systemctl status conduit | head -3
```

`Active: active (running)`. Done.

## Act 2 — the data appears (T+3m → T+8m)

Switch to the Honeycomb tab. Click into Datasets list, sorted by
"last activity":

- Within ~60 seconds, `demo-edge` lights up. Click into it.
- Run a query: metric `system.cpu.utilization`, group by `host.name`.
  Single row of CPU time series.
- Run another: log filter `severity_text != ""`, group by
  `severity_text`. Per-severity log distribution.
- Open the "host overview" dashboard preview if you've imported it
  into the env. Otherwise, point at the four panels you've queried
  and call out "this is not Honeycomb-specific — same `conduit.yaml`
  would land in any OTLP destination."

**Talking points**:

- "Notice none of those queries needed any code. The agent's profile
  loaded host metrics + journald + filelog automatically."
- "Service name = dataset name. `deployment.environment = demo` is on
  every signal as a resource attribute, so I could split this in
  Honeycomb's BubbleUp by environment if I had multiple."

## Act 3 — `conduit doctor` (T+8m → T+13m)

This is the part that lands. The doctor turns the failure modes
people fear about agents (TLS, networking, perms, cardinality) into
concrete, individually-named, individually-fixable codes.

**Happy path** first:

```sh
sudo /usr/bin/conduit doctor -c /etc/conduit/conduit.yaml
```

Walk the output line by line — 5-7 lines, each with a
`CDT0xxx` code. Point out the column structure: severity, code, name,
short summary.

**Then break it on purpose**. Edit the env file:

```sh
sudo sed -i 's|HONEYCOMB_API_KEY=.*|HONEYCOMB_API_KEY=hcaik_wrong|' \
  /etc/conduit/conduit.env
sudo systemctl restart conduit
sudo /usr/bin/conduit doctor -c /etc/conduit/conduit.yaml
```

The output:

```
[FAIL] CDT0102 output.auth: api_key looks like a literal but the destination rejected it
        → https://github.com/conduit-obs/conduit-agent/blob/main/docs/troubleshooting/cdt-codes.md#cdt0102--output-auth
```

Click the URL — open `cdt-codes.md` at the right anchor. Point out:

- "Every CDT code has a docs section. We have a CI test that fails
  the build if a code lands without one."
- "The fix is right here — re-export the env var, restart the
  service. Operators don't have to grep upstream collector source."

Fix it back:

```sh
sudo sed -i "s|HONEYCOMB_API_KEY=.*|HONEYCOMB_API_KEY=$HONEYCOMB_API_KEY|" \
  /etc/conduit/conduit.env
sudo systemctl restart conduit
sudo /usr/bin/conduit doctor -c /etc/conduit/conduit.yaml
```

Green. Move on.

## Act 4 — `conduit preview` and the schema (T+13m → T+18m)

Open `/etc/conduit/conduit.yaml` in `less` on the SSH pane:

```sh
sudo less /etc/conduit/conduit.yaml
```

Point at the size: ~15 lines. Switch to:

```sh
sudo /usr/bin/conduit preview -c /etc/conduit/conduit.yaml | wc -l
sudo /usr/bin/conduit preview -c /etc/conduit/conduit.yaml | head -60
```

~250 lines of fully-formed upstream collector YAML. **Big talking
point**:

- "The 15-line input becomes 250 lines of upstream config. That ratio
  is the value Conduit ships."
- "It's deterministic. Same input → same output, byte for byte. We
  test it with a 10-scenario golden file suite that runs in CI."

If you have time, point out one specific transformation: how
`output.mode: honeycomb` becomes the `otlphttp/honeycomb` exporter
with the right header. Open `docs/reference/configuration.md` in the
browser tab to show the schema.

## Act 5 — switching output mode (T+18m → T+23m)

The "any OTel destination" promise made concrete. Open the config:

```sh
sudo $EDITOR /etc/conduit/conduit.yaml
```

Change:

```yaml
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:HONEYCOMB_API_KEY}
```

To:

```yaml
output:
  mode: otlp
  otlp:
    endpoint: http://localhost:14318         # a local debug receiver
    headers:
      x-honeycomb-team: ${env:HONEYCOMB_API_KEY}
```

Validate **before** restart:

```sh
sudo /usr/bin/conduit config --validate -c /etc/conduit/conduit.yaml
sudo /usr/bin/conduit doctor --check output -c /etc/conduit/conduit.yaml
```

CDT0101 will FAIL because `localhost:14318` isn't running — that's
the point. Talking point:

- "I would have shipped this restart in the old world and only known
  it was broken when traces stopped showing up. Here, doctor told me
  before I restarted."

Revert to honeycomb mode, restart, watch CDT0101 go green again.
(Show the doctor output one more time.)

## Act 6 — `overrides:` and the escape hatch (T+23m → T+27m)

Brief — this is for the people in the room thinking "but what if
upstream has a knob you haven't surfaced?"

Edit the config one more time. Add:

```yaml
overrides:
  receivers:
    hostmetrics:
      collection_interval: 10s
```

Validate, restart, run doctor. Doctor still passes. Talking points:

- "The overrides block is the documented escape hatch. Every upstream
  collector knob is reachable, even ones we haven't promoted to
  first-class fields."
- "We track usage at retro time. If a pattern gets common, it gets
  promoted to a typed field. ADR-0012 has the policy."
- (If you have a minute: show
  [`docs/reference/configuration.md#overrides-optional-map`](../reference/configuration.md#overrides-optional-map)
  for the redaction-processor example.)

## Act 7 — wrap (T+27m → T+30m)

Back to the slide deck:

- The repo: `github.com/conduit-obs/conduit-agent`.
- Getting started: `docs/getting-started/{linux,docker,kubernetes,windows}.md`.
- The full troubleshooting index: `docs/troubleshooting/index.md`.
- "If you want to try it on your own kit: 30 minutes, `--api-key`,
  one curl command. Same recipe on Linux, Docker, Kubernetes, and
  (signed MSI lands soon) Windows."

Open the floor for questions.

---

## Failure-mode playbook

The demo will fail in front of strangers eventually. Have a plan.

| What broke | Recover with |
|---|---|
| **VM is unreachable** (WiFi flaked, instance got reaped) | Switch to the local Docker compose recipe — `cd ~/dev/conduit-agent && docker compose -f deploy/docker/compose-linux-host.yaml up -d`. The Act 1 dialog adapts. |
| **`install_linux.sh` 404s** (release just got cut, asset still uploading) | Pre-stage the `.deb` on the VM — `dpkg -i conduit_*.deb`. Same Act 1 talking points work. |
| **Honeycomb dataset doesn't show up in 60s** | Confirm the env var via `sudo cat /etc/conduit/conduit.env`. If it's right, run `conduit doctor --check output` — that surfaces the issue without making it look like the agent is silently broken. |
| **`conduit doctor` reports something unexpected** (a CDT you didn't plan to demo) | Read it out loud and click the URL. The whole point is that doctor → docs is one click; demonstrating that *when something genuinely surprises you* lands harder than the rehearsed CDT0102 break. |
| **The audience asks "but does it work in air-gapped envs / behind a corporate proxy?"** | Pull up the [Linux troubleshooting "TLS handshake failed" path](../troubleshooting/cdt-codes.md#cdt0101--output-endpoint-reachable). Show the `SSL_CERT_FILE` recipe. |

## What you don't demo

- Code. The talk is for operators, not contributors. Save the
  expander internals, the golden-file harness, etc. for a separate
  contributors' deep-dive.
- ADRs. Reference them ("the design doc is in the repo") but don't
  click through.
- Tail-sampled traces / Refinery routing. They're a separate demo —
  trying to fit a "and here's how Refinery slots in" beat into 30
  minutes makes the whole thing rushed. Mention it as a follow-up
  topic if asked.
- Cardinality denylist. Lives in the troubleshooting docs. Demoing
  it requires a contrived bad config and runs long.

## After the demo

Follow up with anyone who exchanged contact info within 24 hours.
The thing that converts evaluators is the second touch — share the
[`docs/getting-started/linux.md`](../getting-started/linux.md) link
and offer to pair on a real install.

For internal post-mortems, log:

- Total wall-clock time (target: 25 minutes).
- Where the audience asked the most questions (signal for what to
  promote next time).
- Anything that broke and how you recovered (so the playbook above
  grows).
