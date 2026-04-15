# ksync

Go library for syncing Kubernetes custom resources via a PostgreSQL-backed change log.

Callers write desired state through `API`. A `Syncer` polls the DB and drives changes into k8s using Server-Side Apply.

## How it works

```
Your app          PostgreSQL                  Kubernetes
   │                   │                          │
   ├── Apply() ────────► INSERT change row         │
   │                   │                          │
   │              Syncer polls                    │
   │                   ├── read oldest change ────► SSA apply / delete
   │                   │              ◄────────── ok
   │                   ├── DELETE change row       │
   │                   └── UPDATE cr state         │
```

- Changes are append-only. A failed sync leaves the row in place and retries on the next poll.
- One `Syncer` per cluster, all sharing the same DB, each filtered by `cluster`.

## Install

```sh
go get github.com/targc/ksync
```

Requires PostgreSQL. Run the migration:

```sh
psql $DATABASE_URL -f migrations/00001.sql
```

## Usage

```go
import "github.com/targc/ksync"

api := &ksync.API{DB: db}

// Apply a manifest
err := api.Apply(ctx, resourceID, json.RawMessage(`{
    "apiVersion": "apps/v1",
    "kind": "Deployment",
    "metadata": {"namespace": "default", "name": "my-app"},
    "spec": { ... }
}`))

// Delete
err = api.Remove(ctx, resourceID)

// List
var crs []ksync.CustomResource
total, err := api.List(ctx, ksync.ListFilter{
    Cluster: ptr("prod"),
    Kind:    ptr("Deployment"),
}, 1, 20, &crs)

// Start syncer (blocking — run in a goroutine or with context cancellation)
syncer := &ksync.Syncer{
    Cluster:      "prod",
    IntervalSync: 5 * time.Second,
    DB:           db,
    K8s:          k8sClient, // dynamic.Interface
}
syncer.Run(ctx)
```

## API

| Method | Description |
|---|---|
| `Apply(ctx, id, json)` | Upsert a resource and enqueue an apply change |
| `Remove(ctx, id)` | Enqueue a delete change |
| `Get(ctx, id, dest)` | Fetch a resource by ID |
| `List(ctx, filter, page, limit, dest)` | List resources with optional filters |

`ListFilter` fields: `Project`, `Cluster`, `Namespace`, `Kind`, `Search` (name substring).

## Multi-cluster

Run one `Syncer` per cluster:

```go
for _, cluster := range clusters {
    go (&ksync.Syncer{
        Cluster:      cluster.Name,
        IntervalSync: 10 * time.Second,
        DB:           db,
        K8s:          cluster.Client,
    }).Run(ctx)
}
```

## License

MIT
