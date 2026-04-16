# ksync

Sync Kubernetes custom resources via a PostgreSQL-backed change log.

Callers write desired state through the `SDK`. An HTTP API server sits in front of the DB. `Syncer` binaries — one per cluster — poll the API and drive changes into k8s using Server-Side Apply.

## How it works

```
Your app        PostgreSQL        cmd/api (HTTP)       cmd/syncer        k8s
   │                │                   │                   │              │
   ├─ SDK.Create ──►│                   │                   │              │
   │                │                   │                   │              │
   │                │              Syncer polls             │              │
   │                │◄──── GET /changes ─────────────────── │              │
   │                │─── []SyncChange ──────────────────── ►│              │
   │                │                   │                   ├─ SSA apply ─►│
   │                │                   │                   │◄──── ok ─────┤
   │                │◄─ POST /success ──────────────────────│              │
   │                │  DELETE change row│                   │              │
   │                │  UPDATE cr state  │                   │              │
```

- Failed syncs leave the change row in place — retried on the next poll.
- One Syncer per cluster, each authenticated with a per-cluster API token.
- Syncers never touch the DB directly.

## Packages

| Import path | Description |
|---|---|
| `github.com/targc/ksync/pkg` | Public library — `SDK`, models, `ListFilter` |
| `github.com/targc/ksync/internal/apiserver` | Fiber HTTP server (internal) |
| `github.com/targc/ksync/internal/syncer` | Syncer HTTP client + k8s ops (internal) |

## Setup

### 1. Run migrations

```sh
DATABASE_URL=postgres://... go run ./cmd/migrate
```

### 2. Start the API server

```sh
DATABASE_URL=postgres://... PORT=8080 go run ./cmd/api
```

### 3. Create an API token

```sql
INSERT INTO ksync_api_tokens (id, token, cluster)
VALUES (gen_random_uuid(), 'my-secret-token', 'prod');
```

### 4. Start a Syncer

```sh
API_URL=http://localhost:8080 \
API_TOKEN=my-secret-token \
INTERVAL_SYNC=5s \
KUBECONFIG=~/.kube/config \
go run ./cmd/syncer
```

## SDK Usage

```go
import ksync "github.com/targc/ksync/pkg"

sdk := &ksync.SDK{DB: db}

// Create a resource and enqueue an apply
err := sdk.Create(ctx, resourceID, "prod", json.RawMessage(`{
    "apiVersion": "apps/v1",
    "kind": "Deployment",
    "metadata": {"namespace": "default", "name": "my-app"},
    "spec": { ... }
}`))

// Update (enqueues another apply)
err = sdk.Update(ctx, resourceID, newJSON)

// Delete
err = sdk.Remove(ctx, resourceID)

// List
var crs []ksync.CustomResource
total, err := sdk.List(ctx, ksync.ListFilter{
    Cluster: ptr("prod"),
    Kind:    ptr("Deployment"),
}, 1, 20, &crs)

// Get
var cr ksync.CustomResource
err = sdk.Get(ctx, resourceID, &cr)
```

## Docker

Images are published to GitHub Container Registry on every push to `main`:

```
ghcr.io/<owner>/ksync-api:latest
ghcr.io/<owner>/ksync-syncer:latest
```

Build locally:

```sh
docker build -f Dockerfile.api    -t ksync-api    .
docker build -f Dockerfile.syncer -t ksync-syncer .
```

## HTTP API

All routes require `Authorization: Bearer <token>`.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/changes` | Oldest pending change per resource (max 100) |
| `POST` | `/api/v1/changes/:id/syncing` | Mark change as in-flight |
| `POST` | `/api/v1/changes/:id/success` | Confirm applied — deletes change row, updates CR |
| `POST` | `/api/v1/changes/:id/error` | Report failure — clears lock, sets error |

## License

MIT
