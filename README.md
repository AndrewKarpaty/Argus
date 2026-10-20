# Argus

An AI platform for incident investigation that reasons about service dependencies.

When `checkout` and `orders` start alerting at the same time, the useful answer is not
"both are returning 5xx". It is "they share a dependency on `postgres-main`, a connection
pool deploy landed there 8 minutes ago, here is the propagation path, and here is what to
do about it". Argus produces that answer by keeping a service dependency graph and
reasoning over the topology, rather than over log text.

**This is a skeleton.** The domain model, the dependency graph, the correlation algorithm
and the agentic loop are fully implemented. Telemetry sources and persistence sit behind
interfaces with default implementations - a synthetic provider and in-memory stores - so
the whole thing runs end to end out of the box.

## Quick start

```bash
# Investigate from the command line: no server, no credentials
make demo

# Or run the API with the web console
make ui       # build the console into web/dist (needs Node 18+)
make run      # http://localhost:8080
```

`argusd` serves the API and the console from one binary and one port: the
console is embedded at build time. Without `make ui` the binary still starts
and serves the API, and says so in the log.

Open an incident and investigate it in one call:

```bash
curl -s -X POST localhost:8080/api/v1/incidents \
  -H 'Content-Type: application/json' \
  -d '{"title":"Checkout 5xx spike","severity":"critical",
       "affected_services":["checkout","orders"],"investigate":true}'

# Investigations run in the background: the response carries the id, poll it for the result
curl -s localhost:8080/api/v1/investigations/<id>
```

To use the Claude agent, a key is all it takes - the engine picks it up on its own
(`ARGUS_ENGINE=auto`):

```bash
export ANTHROPIC_API_KEY=sk-ant-...
go run ./cmd/argusctl investigate -service checkout -service orders -v
```

Without credentials the platform runs the deterministic graph heuristic, which is also the
baseline the model-driven engine has to beat.

## How it works

```
alert / API ──► Incident ──► Engine ──► Reasoner ──► Hypotheses
                               │           │
                               │           ├── graph: suspects, blast radius, paths
                               │           └── telemetry: metrics, logs, traces, changes
                               └── Catalog (YAML → graph.Graph)
```

The propagation model: an edge `A → B` means "A calls B", so faults travel **against the
arrows** - a fault in `B` surfaces as symptoms in `A`. Three operations follow from that,
and the whole platform rests on them:

| Operation | The question it answers |
|---|---|
| `Suspects(affected)` | which shared upstream dependencies explain **all** the symptoms at once |
| `BlastRadius(service)` | what is at risk while this service is unhealthy |
| `Path(from, to)` | whether the propagation route we are about to claim actually exists |

`criticality: hard|soft` on an edge is the most valuable piece of catalog metadata: a hard
dependency takes its caller down with it, a soft one only degrades it - so a soft-only path
cannot explain a full outage.

### Two engines

**`heuristic`** - no model, no network. It ranks shared upstream nodes (symptom coverage ×
hop distance × criticality × tier × node kind), then weighs each candidate against the
telemetry in the incident window. Deterministic, fast, testable.

**`claude`** ([`internal/agent`](internal/agent/)) - an agentic loop: the model explores the
graph and the telemetry through tools instead of being handed a fixed context. The tools
are `rank_suspects`, `get_service_topology`, `list_dependencies`, `blast_radius`,
`dependency_path`, `get_telemetry`, and the terminal `report_findings`.

Two decisions in the agent worth knowing about:

- **`report_findings` is the only way to answer.** Hypotheses arrive as a validated tool
  call, not as text to be parsed. If the model names a service that is not in the catalog,
  or a hypothesis without evidence, the call is handed back to it as a tool error and it
  corrects itself within the same loop.
- **The system prompt is cached**, and everything incident-specific lives in the user
  message after that cache breakpoint. The prompt carries the 2-hop neighbourhood of the
  affected services, not the whole catalog.

`ARGUS_MAX_ITERATIONS` bounds the number of turns in the loop, which is the real cost
control.

## Layout

```
cmd/argusd         HTTP server
cmd/argusctl       CLI: investigate, graph, suspects
internal/domain    types: Service, Dependency, Incident, Signal, Hypothesis…
internal/graph     the dependency graph: traversals, suspects, cycles, mermaid
internal/catalog   catalog sources (YAML) + a Provider with hot reload
internal/telemetry provider interfaces + Collector (concurrent fan-out)
internal/investigator  Engine (lifecycle) + Heuristic (reasoner)
internal/agent     the Claude reasoner: prompt, tools, loop
internal/store     store interfaces + in-memory implementations
internal/api       HTTP: routing, handlers, middleware
web/               the console (React + TypeScript), embedded into argusd
configs/           sample service catalog and environment file
```

## The console

Three screens, dark by default because this is a thing people stare at during
an incident, with a light theme selected step by step rather than inverted:

- **Incidents** — what is open, what is being investigated, and the leading
  hypothesis per row with its confidence.
- **Investigation** — the verdict, the ranked hypotheses with their evidence
  and propagation paths, and beside them *How it got there*: every tool call
  the run made, in order, with what each one returned. A conclusion you cannot
  audit is not worth much at 3am, so the trail sits next to the answer rather
  than under it. While a run is in flight the page polls and fills in live.
- **Topology** — the dependency graph, laid out by longest path so faults
  visibly travel against the arrows. It opens on the service the newest
  incident blames and draws only what has a dependency path to it, naming
  what it hid; *Whole catalog* is one click away. Under it, the correlation
  table: pick the services that are misbehaving and see what they share
  upstream, and why each candidate ranks where it does.

Two deliberate omissions in `web/`: there is no router library (three routes
and one parameter did not justify a dependency that needs patching, so
`src/lib/router.tsx` does it in 60 lines of History API), and no test runner
(every advisory-free vitest needs Node 20). What the console does have is
`npm test`: a typecheck plus executable checks on the graph layout, which is
the part where a wrong number means overlapping boxes or arrows through the
middle of a card.

## API

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthz`, `/readyz` | liveness; readiness with graph size and wired providers |
| `POST` | `/api/v1/incidents` | open an incident (`"investigate": true` starts a run immediately) |
| `GET` | `/api/v1/incidents` | list; filters `status`, `service`, `limit` |
| `GET` | `/api/v1/incidents/{id}` | the incident, its window and all its investigations |
| `POST` | `/api/v1/incidents/{id}/investigate` | start an investigation → `202` |
| `GET` | `/api/v1/investigations/{id}` | status, hypotheses, evidence, audit trail, token usage |
| `GET` | `/api/v1/services` | the catalog |
| `GET` | `/api/v1/services/{id}` | one service with its direct dependencies and dependents |
| `GET` | `/api/v1/services/{id}/dependencies` | traversal: `direction=dependencies\|dependents`, `depth` |
| `GET` | `/api/v1/services/{id}/blast-radius` | what breaks when this service fails |
| `GET` | `/api/v1/graph` | the graph: `format=json\|mermaid\|text`, `service`, `radius`, `highlight` |
| `GET` | `/api/v1/suspects` | rank what a set of symptomatic services share upstream: `service` (repeat), `depth`, `limit` |
| `POST` | `/api/v1/catalog/refresh` | reload the catalog (on failure the previous one keeps serving) |
| `POST` | `/api/v1/webhooks/alerts` | alert intake → incident → investigation |

## Where to plug your own things in

**Telemetry.** Four narrow interfaces in
[`internal/telemetry`](internal/telemetry/telemetry.go): `MetricsProvider`, `LogsProvider`,
`TracesProvider`, `ChangeProvider`. They ask "what looks wrong with this service in this
window", not "run this PromQL", which keeps Prometheus, Datadog, Loki and Tempo
interchangeable and keeps query syntax out of the prompt. Swap them in `buildCollector` in
[cmd/argusd/main.go](cmd/argusd/main.go). `ChangeProvider` is the one worth wiring first:
most incidents start with a deploy.

**Catalog.** `catalog.Source` has a single `Load` method. A file in the repository is the
cheapest way to start - it is reviewable, diffable and needs no control plane - with
Kubernetes, an OpenTelemetry service map or Backstage as the next step.

**Persistence.** `store.Incidents` and `store.Investigations`. The in-memory
implementations lose everything on restart; Postgres is a new file in that package and a
wiring change.

## Configuration

Every variable is listed in [configs/argus.env.example](configs/argus.env.example), and
every one has a working default. The ones that matter most: `ARGUS_ENGINE`
(`auto`/`claude`/`heuristic`), `ANTHROPIC_API_KEY`, `ARGUS_MODEL`, `ARGUS_EFFORT`,
`ARGUS_MAX_ITERATIONS`, `ARGUS_CATALOG_PATH`, `ARGUS_DEMO_TELEMETRY`.

## Development

```bash
make test        # Go tests
make test-race   # the same, under the race detector
make ui-test     # typecheck the console + graph-layout checks
make all         # fmt + vet + test + build
make ui-dev      # console with hot reload, proxying the API to :8080
make release     # console + binaries with it embedded
make docker
```

The Go tests cover the graph algorithms, the correlation ranking, the full incident →
investigation path, and the agentic loop - the last one against a fake API server, so no
request ever reaches the real API.

Working on the console: run `make run` in one terminal and `make ui-dev` in another, then
open the Vite server on :5173. It proxies `/api` and `/readyz` to argusd, so hot reload
works against real data.

## What is deliberately missing

These are the next steps, not hidden gaps:

- **Persistence** - Postgres instead of memory, with migrations.
- **Topology discovery** - build the graph from traces or a service mesh, and diff the
  declared catalog against what is actually observed.
- **Learning from history** - find similar past incidents and what resolved them.
- **Quality measurement** - an eval set of real incidents with known causes. Without one,
  "the model got better" is a feeling rather than a fact.
- **Component tests for the console** — the layout engine has executable checks, but the
  React components have none; a runner needs Node 20 to come without advisories.
- **Authentication and rate limiting** on the API, plus an audit trail of who ran what.
- **Actions with consequences** (rollback, traffic shifting). For now the platform only
  suggests next steps, which is the right boundary to start from.
