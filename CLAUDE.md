# ksync

Go library + HTTP API for syncing Kubernetes custom resources via a DB-backed change log.
Callers write changes through `SDK`; a `Syncer` binary polls the HTTP API and drives them into k8s.

---

## Direction

ksync decouples **intent** (what k8s state should exist) from **execution** (actually calling the k8s API).

**The database is the source of truth, not k8s.** Callers never talk to k8s directly. They write a desired manifest into the DB via `SDK`, and the `Syncer` eventually reconciles k8s to match.

- **Audit log** — every change is a row before it's applied
- **Retry for free** — failed changes stay in the table and get retried on the next poll
- **Multi-cluster fan-out** — one Syncer per cluster, each authenticated with its own API token
- **Decoupled availability** — callers succeed even when k8s is temporarily unreachable
- **Untrusted syncers** — syncers run in partner clusters and talk to the HTTP API; they never touch the DB directly

Evolution path:
1. ~~Single-process library~~ (done)
2. ~~HTTP API decoupling~~ (done — `cmd/api` + token auth)
3. Next: replace polling with DB LISTEN/NOTIFY or a queue for lower latency

---

## Architecture

```
Your Application
│
├── SDK.Create / Update / Remove
│        │
│        ▼
│   PostgreSQL
│   ├── ksync_custom_resources
│   ├── ksync_change_custom_resources
│   └── ksync_api_tokens
│        │
│        │  HTTP (Bearer token)
│        ▼
│   cmd/api  (Fiber HTTP server)
│   GET  /api/v1/changes
│   POST /api/v1/changes/:id/syncing
│   POST /api/v1/changes/:id/success
│   POST /api/v1/changes/:id/error
│        │
│        ▼
│   cmd/syncer  (one per cluster)
│   └── polls changes → applies to k8s via SSA
│        │
│        ▼
│   Kubernetes (dynamic client, SSA)
```

### Multi-cluster

One Syncer binary per cluster. Each has its own API token row (`ksync_api_tokens`) that maps token → cluster. All write through the same API server and DB.

```
                    ┌─────────────┐
                    │  cmd/api    │
                    └──────┬──────┘
           ┌───────────────┼───────────────┐
           │               │               │
    ┌──────▼──────┐ ┌──────▼──────┐ ┌──────▼──────┐
    │   Syncer    │ │   Syncer    │ │   Syncer    │
    │ cluster=dev │ │cluster=prod │ │ cluster=stg │
    └──────┬──────┘ └──────┬──────┘ └──────┬──────┘
           │               │               │
        k8s-dev         k8s-prod        k8s-stg
```

---

## Concepts

### Change Log
Every mutation is a `ksync_change_custom_resources` row. The syncer processes the oldest pending change per resource, applies it to k8s via the HTTP API success/error endpoints, which then delete the row. If k8s fails, the row stays — next poll retries.

### Intent vs State
- `CustomResource.JSON` — last successfully synced manifest (what k8s has)
- `ChangeCustomResource.JSON` — desired manifest (what caller wants)

### One Change Per Resource Per Cycle
`DISTINCT ON (custom_resource_id) ORDER BY id` picks the oldest pending change per resource. Newer changes queue behind it.

### Soft Lock
`syncing_change_custom_resource_id` marks the in-flight change — advisory only, not checked before processing. Overwritten on next cycle after a crash.

### Server-Side Apply
All applies use SSA (`FieldManager: "ksync"`, `Force: true`). Callers send full manifests; ksync does not diff or patch.

---

## applyChange State Machine (via HTTP)

```
Syncer                          API Server                  k8s
  │                                  │                        │
  ├─ GET /changes ──────────────────►│                        │
  │ ◄─────────────── []SyncChange ───┤                        │
  │                                  │                        │
  ├─ POST /changes/:id/syncing ─────►│ set syncing lock       │
  │                                  │                        │
  ├─ k8sApply / k8sDelete ──────────────────────────────────►│
  │ ◄──────────────────────────────────────── ok / error ─────┤
  │                                  │                        │
  ├─ POST /changes/:id/success ─────►│ DELETE change row      │
  │   OR                             │ UPDATE cr state        │
  └─ POST /changes/:id/error ───────►│ clear lock, set error  │
```

---

## Module

```
module  github.com/targc/ksync
go      1.25
```

Key deps: `gorm.io/gorm`, `github.com/gofiber/fiber/v3`, `k8s.io/client-go/dynamic`, `k8s.io/apimachinery`, `github.com/google/uuid`

---

## File Structure

```
ksync/
├── pkg/                          # public library (package ksync)
│   ├── ksync.go                  # all types and models
│   └── sdk.go                    # SDK methods — DB CRUD + change enqueue
├── internal/
│   ├── apiserver/                # Fiber HTTP server (package apiserver)
│   │   ├── server.go             # Server, SetupRoutes, authMiddleware
│   │   ├── handler_changes_list.go
│   │   ├── handler_changes_syncing.go
│   │   ├── handler_changes_success.go
│   │   └── handler_changes_error.go
│   └── syncer/                   # Syncer HTTP client + k8s ops (package syncer)
│       └── syncer.go
├── cmd/
│   ├── api/main.go               # API server binary
│   ├── syncer/main.go            # Syncer binary
│   └── migrate/main.go           # DB migration runner
├── migrations/
│   ├── 00001.sql                 # custom_resources + change_custom_resources
│   └── 00002.sql                 # ksync_api_tokens
├── Dockerfile.api
├── Dockerfile.syncer
└── .github/workflows/ci.yml
```

---

## Types (`pkg/ksync.go`)

### `CustomResource` → table `ksync_custom_resources`

| Column | Notes |
|---|---|
| `id` uuid PK | caller-assigned, stable across updates |
| `project`, `cluster` | routing/filtering |
| `api_version`, `kind`, `namespace`, `name` | identity — populated from JSON on every Create/Update |
| `json` jsonb | last successfully synced manifest |
| `syncing_change_custom_resource_id` | soft lock: set to in-flight change ID |
| `last_change_custom_resource_id` | ID of last successfully applied change |
| `last_sync_error` | cleared on success, set on k8s error |
| `deleted_at` | soft-delete |

### `ChangeCustomResource` → table `ksync_change_custom_resources`

Append-only log. Rows are deleted after successful sync.

| Field | Notes |
|---|---|
| `id` uuid PK | |
| `custom_resource_id` | FK to `ksync_custom_resources`, indexed |
| `json` jsonb | full manifest for `apply`; empty for `delete` |
| `action` | `"apply"` or `"delete"` |

### `ApiToken` → table `ksync_api_tokens`

| Field | Notes |
|---|---|
| `id` uuid PK | |
| `token` | unique bearer token |
| `cluster` | cluster name this token grants access to |

### `SyncChange`

Response type for `GET /changes`. Embeds `ChangeCustomResource` plus CR identity fields (`CRAPIVersion`, `CRKind`, `CRNamespace`, `CRName`) so the syncer can call k8sDelete without re-fetching the DB.

### `SDK`

```go
SDK{DB: *gorm.DB}
```

---

## SDK Methods (`pkg/sdk.go`)

| Method | Description |
|---|---|
| `Create(ctx, id, cluster, json)` | Insert CR + enqueue apply change |
| `Update(ctx, id, json)` | Enqueue apply change for existing CR |
| `Remove(ctx, id)` | Enqueue delete change |
| `Get(ctx, id, dest)` | Fetch CR by ID |
| `List(ctx, filter, page, limit, dest)` | List CRs with filters |

`ListFilter` fields: `Project`, `Cluster`, `Namespace`, `Kind`, `Search` (name ILIKE).

---

## HTTP API Routes (`internal/apiserver/`)

All routes require `Authorization: Bearer <token>`. Token is looked up in `ksync_api_tokens`; the matching `cluster` is injected via `c.Locals`.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/changes` | Oldest pending change per resource for this cluster (max 100) |
| `POST` | `/api/v1/changes/:id/syncing` | Set soft lock on the CR |
| `POST` | `/api/v1/changes/:id/success` | Delete change row, update CR state |
| `POST` | `/api/v1/changes/:id/error` | Clear lock, set `last_sync_error` |

### `POST /changes/:id/success` behaviour
- Deletes the change row
- If `action=apply`: sets `cr.json = change.json`
- If `action=delete`: sets `cr.deleted_at = now()`
- Always: clears `syncing_change_custom_resource_id`, sets `last_change_custom_resource_id`, clears `last_sync_error`

---

## Syncer (`internal/syncer/syncer.go`)

```go
Syncer{
    APIURL       string
    APIToken     string
    IntervalSync time.Duration
    K8s          dynamic.Interface
}
```

No DB access. Polls `GET /changes`, applies each change to k8s, reports success or error via HTTP.

### k8s Integration
- `k8sApply` — SSA via `dynamic.Resource(gvr).Namespace(ns).Apply(...)`
- `k8sDelete` — `dynamic.Resource(gvr).Namespace(ns).Delete(...)`
- `parseGVR` — naive `strings.ToLower(kind) + "s"` pluralization

---

## env Config

### `cmd/api`
| Var | Default | Notes |
|---|---|---|
| `DATABASE_URL` | required | PostgreSQL DSN |
| `PORT` | `8080` | |

### `cmd/syncer`
| Var | Default | Notes |
|---|---|---|
| `API_URL` | required | Base URL of `cmd/api` |
| `API_TOKEN` | required | Token from `ksync_api_tokens` |
| `INTERVAL_SYNC` | `5s` | Poll cadence (Go duration string) |
| `KUBECONFIG` | — | Falls back to in-cluster config |

### `cmd/migrate`
| Var | Notes |
|---|---|
| `DATABASE_URL` | PostgreSQL DSN |

---

## Known Limitations

- **Naive pluralization** — `kind + "s"` breaks for irregular plurals (`Ingress` → `ingresss`). Fix: discovery client or lookup table.
- **No cluster-scoped resource support** — `k8sDelete` always calls `.Namespace(ns)`. Pass `""` for cluster-scoped resources.
- **Soft lock is advisory** — not checked before processing; overwritten on next poll after crash.
- **Batch size fixed at 100** — hardcoded in SQL. Make `Syncer.BatchSize` configurable if needed.
- **Sequential processing** — one change at a time. Add a worker pool for throughput.
- **PostgreSQL-specific** — uses `DISTINCT ON` and `ILIKE`.
