# ksync

Go library for syncing Kubernetes custom resources via a DB-backed change-log.
Callers write changes through `API`; `Syncer` polls and drives them into k8s.

---

## Direction

ksync exists to decouple **intent** (what k8s state should exist) from **execution** (actually calling the k8s API).

The core bet is: **the database is the source of truth, not k8s**. Callers never talk to k8s directly. They write a desired manifest into the DB, and ksync eventually reconciles k8s to match. This gives you:

- **Audit log** — every change is a row before it's applied
- **Retry for free** — failed changes stay in the table and get retried on the next poll
- **Multi-cluster fan-out** — run one `Syncer` per cluster, all reading from the same DB, each filtered by `cluster`
- **Decoupled availability** — callers succeed even when k8s is temporarily unreachable

The intended evolution path:
1. Now: single-process library, caller embeds `API` and `Syncer`
2. Next: expose `API` over HTTP/gRPC so multiple services can write changes
3. Later: replace polling with DB LISTEN/NOTIFY or a queue for lower latency

---

## Concepts

### Change Log
Every mutation is recorded as a `ChangeCustomResource` row before anything touches k8s.
The syncer processes the oldest pending change per resource, applies it, then deletes the row.
If k8s fails, the row stays — next poll retries automatically.

```
Caller            DB                        k8s
  │                │                          │
  ├─ Apply() ─────►│ INSERT change row         │
  │                │                          │
  │           Syncer polls...                 │
  │                │                          │
  │                ├─ read oldest change ─────►│ SSA apply/delete
  │                │          ◄───────────────┤ ok
  │                ├─ DELETE change row        │
  │                ├─ UPDATE cr.json           │
```

### Intent vs State
`CustomResource.JSON` is the **last successfully synced manifest** — what k8s currently has (as far as we know).
`ChangeCustomResource.JSON` is the **desired manifest** — what the caller wants k8s to have.
They diverge between the time a change is written and the time the syncer processes it.

```
┌─────────────────────────────────────────────┐
│ custom_resources                            │
│                                             │
│  json ──────────► "what k8s has now"        │
│                   (updated after sync)      │
└─────────────────────────────────────────────┘

┌─────────────────────────────────────────────┐
│ change_custom_resources                     │
│                                             │
│  json ──────────► "what caller wants"       │
│                   (deleted after sync)      │
└─────────────────────────────────────────────┘
```

### One Change Per Resource Per Cycle
The SQL query uses `DISTINCT ON (custom_resource_id) ORDER BY id` — this picks the **oldest** pending change per resource. Newer changes for the same resource are queued behind it.

This means rapid updates to one resource do not starve other resources. Each resource gets exactly one attempt per sync cycle.

### Soft Lock
`syncing_change_custom_resource_id` marks which change is in-flight. It is **advisory only** — not checked before processing, just written for observability. After a crash it stays set until overwritten on the next cycle. Safe to ignore for correctness, useful for debugging stuck resources.

### Identity Fields
`api_version`, `kind`, `namespace`, `name` are stored as columns on `custom_resources` so the syncer can delete a resource without loading and parsing its JSON. These are kept up to date by `Apply` on every call via `Assign(...).FirstOrCreate`.

### Server-Side Apply
All apply operations use SSA (`Force: true`, `FieldManager: "ksync"`). This means:
- ksync owns the fields it manages; conflicts with other managers are force-resolved
- Callers send full manifests — ksync does not diff or patch
- k8s handles idempotency — applying the same manifest twice is safe

---

## Architecture Diagram

```
┌──────────────────────────────────────────────────────────────────┐
│                        Your Application                          │
│                                                                  │
│   ksync.API                                                      │
│   ├── Apply(id, json)  ─────────────────────────────────────┐   │
│   ├── Remove(id)       ─────────────────────────────────────┤   │
│   ├── List(filter)     ◄── reads custom_resources           │   │
│   └── Get(id)          ◄── reads custom_resources           │   │
└────────────────────────────────────────────────────────────────┬─┘
                                                                 │
                              ┌──────────────────────────────────▼───┐
                              │           PostgreSQL                  │
                              │                                       │
                              │  custom_resources                     │
                              │  ┌────────────────────────────────┐  │
                              │  │ id | cluster | kind | name ... │  │
                              │  │ id | cluster | kind | name ... │  │
                              │  └────────────────────────────────┘  │
                              │                                       │
                              │  change_custom_resources              │
                              │  ┌────────────────────────────────┐  │
                              │  │ id | cr_id | action | json     │  │
                              │  │ id | cr_id | action | json     │  │
                              │  └────────────────────────────────┘  │
                              └──────────┬────────────────────────────┘
                                         │ poll every IntervalSync
                              ┌──────────▼────────────────────────────┐
                              │           ksync.Syncer                │
                              │   cluster = "prod"                    │
                              │                                       │
                              │   sync()                              │
                              │   └─ oldest change per resource       │
                              │        ├─ apply  → k8sApply (SSA)     │
                              │        └─ delete → k8sDelete          │
                              └──────────┬────────────────────────────┘
                                         │
                              ┌──────────▼────────────────────────────┐
                              │        Kubernetes (dynamic client)    │
                              │                                       │
                              │   cluster: prod                       │
                              │   Resources: Deployment, Service ...  │
                              └───────────────────────────────────────┘
```

### Multi-cluster setup

Run one `Syncer` per cluster. All syncers share the same DB; each filters by `cluster`.

```
                    ┌─────────────┐
                    │  PostgreSQL │
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

## applyChange State Machine

```
                    ┌─────────────────────┐
                    │  change row exists  │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │  set syncing_id     │  ← soft lock
                    └──────────┬──────────┘
                               │
              ┌────────────────┴─────────────────┐
              │ action=apply                      │ action=delete
              ▼                                   ▼
    ┌─────────────────┐               ┌───────────────────────┐
    │  k8sApply(json) │               │  fetch CR identity    │
    │  (SSA)          │               │  k8sDelete(av,k,ns,n) │
    └────────┬────────┘               └──────────┬────────────┘
             │                                   │
             └─────────────┬─────────────────────┘
                           │
              ┌────────────┴──────────────┐
              │ k8s error?                │
              │                           │
         YES  ▼                      NO   ▼
    ┌──────────────────┐      ┌──────────────────────────┐
    │ clear lock       │      │ DELETE change row         │
    │ write sync_error │      │ UPDATE cr:                │
    │ return error     │      │   json (if apply)         │
    │ (row stays,      │      │   last_change_id          │
    │  retried next    │      │   clear sync_error        │
    │  poll)           │      │   clear lock              │
    └──────────────────┘      └──────────────────────────┘
```

---

## Module

```
module  github.com/targc/ksync
package ksync
go      1.25
```

Key deps: `gorm.io/gorm`, `k8s.io/client-go/dynamic`, `k8s.io/apimachinery`, `github.com/google/uuid`

---

## Files

| File | Responsibility |
|---|---|
| `ksync.go` | All types and structs |
| `api.go` | `API` methods — CRUD + change enqueue |
| `syncer.go` | Poll loop, k8s apply/delete, GVR helpers |

---

## Types

### `CustomResource` → table `custom_resources`

| Column | Notes |
|---|---|
| `id` uuid PK | caller-assigned, stable across updates |
| `project`, `cluster` | routing/filtering |
| `api_version`, `kind`, `namespace`, `name` | identity — populated from JSON on every `Apply`, used for delete without re-parsing JSON |
| `json` jsonb | last successfully synced manifest |
| `syncing_change_custom_resource_id` | set to the in-flight change ID as a soft lock |
| `last_change_custom_resource_id` | ID of last successfully applied change |
| `last_sync_error` | cleared on success, set on k8s error |
| `deleted_at` | soft-delete |

### `ChangeCustomResource` → table `change_custom_resources`

Append-only log. Rows are deleted after successful sync.

| Field | Notes |
|---|---|
| `id` uuid PK | |
| `custom_resource_id` | FK to `custom_resources`, indexed |
| `json` jsonb | full manifest for `apply`; empty for `delete` |
| `action` | `"apply"` or `"delete"` |

### `Syncer`

```go
Syncer{
    Cluster      string           // filters which CRs to process
    IntervalSync time.Duration    // poll cadence
    DB           *gorm.DB
    K8s          dynamic.Interface
}
```

### `API`

```go
API{DB: *gorm.DB}
```

---

## API Methods

### `Apply(ctx, customResourceID, jsn)`
- Parses `apiVersion`, `kind`, `metadata.namespace`, `metadata.name` from JSON
- Upserts `CustomResource` with `Assign(...).FirstOrCreate` — identity fields always updated
- Appends `ChangeCustomResource{action: "apply", json: jsn}`

### `Remove(ctx, id)`
- Appends `ChangeCustomResource{action: "delete", json: nil}`
- Does **not** delete the `CustomResource` row

### `List(ctx, filter, page, limit, dest)`
- Filters: `project`, `cluster`, `namespace`, `kind`, `search` (name ILIKE)
- 1-based pagination

### `Get(ctx, id, dest)`
- Fetches by `id`

---

## k8s Integration

### `k8sApply`
- Unmarshals full JSON into `unstructured.Unstructured`
- Uses Server-Side Apply (`FieldManager: "ksync"`, `Force: true`)

### `k8sDelete`
- Takes explicit string params — no JSON unmarshal
- Derives GVR: `strings.ToLower(kind) + "s"` (naive pluralization)
- Calls `dynamic.Resource(gvr).Namespace(ns).Delete(...)`

### `parseGVR(obj)`
- Used only by `k8sApply`
- Returns `(GroupVersionResource, namespace, name, error)`

---

## Known Limitations / Things to Watch

- **Naive pluralization** — `kind + "s"` breaks for irregular plurals (e.g. `Ingress` → `ingresss`). Fix: use a discovery client or a lookup table.
- **No cluster-level resource support** — `k8sDelete` always calls `.Namespace(namespace)`. For cluster-scoped resources pass `""` as namespace (works with the dynamic client, just ensure callers set it correctly).
- **No soft-delete** — `Remove` enqueues a delete change but the `CustomResource` row remains with `DeletedAt = nil`.
- **Soft lock is advisory** — `syncing_change_custom_resource_id` is set but not checked before processing. A crash mid-sync leaves it set; overwritten on next poll.
- **Batch size fixed at 100** — hardcoded in the raw SQL. Make `Syncer.BatchSize` configurable if needed.
- **Sequential processing** — changes are applied one by one. Parallelise with a worker pool if throughput matters.
- **`json.Unmarshal` error ignored in `Apply`** — malformed JSON results in empty identity fields, CR still upserted.
- **DB is PostgreSQL-specific** — uses `DISTINCT ON` and `ILIKE`.

---

## DB Migration Notes

Adding `api_version` column (added Apr 2026):

```sql
ALTER TABLE custom_resources ADD COLUMN api_version TEXT NOT NULL DEFAULT '';
```

Existing rows will have empty `api_version` until their next `Apply` call repopulates it.
Rows with empty `api_version` that receive a `delete` change will fail at `schema.ParseGroupVersion("")`.

---

## Usage Example

```go
db  := // *gorm.DB (PostgreSQL)
k8s := // dynamic.Interface built from rest.Config

api := &ksync.API{DB: db}

// enqueue an apply
_ = api.Apply(ctx, resourceID, json.RawMessage(`{
    "apiVersion": "apps/v1",
    "kind": "Deployment",
    "metadata": {"namespace": "default", "name": "my-app"}
}`))

// start syncer (blocking, cancel ctx to stop)
syncer := &ksync.Syncer{
    Cluster:      "prod",
    IntervalSync: 5 * time.Second,
    DB:           db,
    K8s:          k8s,
}
syncer.Run(ctx)
```
