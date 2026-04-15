package onelink

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Syncer struct {
	Cluster string
}

func (s *Syncer) Run(ctx context.Context) error {
	return nil
}

type IResource interface {
	ToNativeK8sJSON() (string, error)
	MarshalJSON() ([]byte, error)
	UnmarshalJSON([]byte) error
}

type CustomResource struct {
	ID                            uuid.UUID // uuid v7
	Project                       string
	Cluster                       string
	Namespace                     string
	Kind                          string
	Name                          string
	JSON                          IResource
	SyncingChangeCustomResourceID *uuid.UUID // uuid v7
	LastChangeCustomResourceID    uuid.UUID  // uuid v7
	LastSyncError                 *string
	CreatedAt                     time.Time
	UpdatedAt                     time.Time
	DeletedAt                     *time.Time
}

type ChangeCustomResourceAction string

var (
	ChangeCustomResourceActionApply  ChangeCustomResourceAction = "apply"
	ChangeCustomResourceActionDelete ChangeCustomResourceAction = "delete"
)

type ChangeCustomResource struct {
	ID               uuid.UUID // uuid v7
	CustomResourceID uuid.UUID // uuid v7
	JSON             IResource // null for action=delete
	Action           ChangeCustomResourceAction
	CreatedAt        time.Time
}

type API struct {
}

type ListFilter struct {
	Project   *string
	Cluster   *string
	Namespace *string
	Kind      *string
	Search    *string
}

func (a *API) List(ctx context.Context, filter ListFilter, page, limit int64, dest []CustomResource) (int64, error) {
	return 0, nil
}

func (a *API) Get(ctx context.Context, id uuid.UUID, dest *CustomResource) error {
	return nil
}

func (a *API) Apply(ctx context.Context, customResourceID uuid.UUID, jsn IResource) error {
	return nil
}

func (a *API) Remove(ctx context.Context, id uuid.UUID) error {
	return nil
}
