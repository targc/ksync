package ksync

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"k8s.io/client-go/dynamic"
)

type IResource = json.RawMessage

type Syncer struct {
	Cluster      string
	IntervalSync time.Duration
	DB           *gorm.DB
	K8s          dynamic.Interface
}

type CustomResource struct {
	ID                            uuid.UUID  `gorm:"type:uuid;primaryKey"`
	Project                       string
	Cluster                       string
	APIVersion                    string
	Kind                          string
	Namespace                     string
	Name                          string
	JSON                          IResource  `gorm:"type:jsonb"`
	SyncingChangeCustomResourceID *uuid.UUID `gorm:"type:uuid"`
	LastChangeCustomResourceID    uuid.UUID  `gorm:"type:uuid"`
	LastSyncError                 *string
	CreatedAt                     time.Time
	UpdatedAt                     time.Time
	DeletedAt                     *time.Time
}

func (CustomResource) TableName() string { return "custom_resources" }

type ChangeCustomResourceAction string

var (
	ChangeCustomResourceActionApply  ChangeCustomResourceAction = "apply"
	ChangeCustomResourceActionDelete ChangeCustomResourceAction = "delete"
)

type ChangeCustomResource struct {
	ID               uuid.UUID                  `gorm:"type:uuid;primaryKey"`
	CustomResourceID uuid.UUID                  `gorm:"type:uuid;index"`
	JSON             IResource                  `gorm:"type:jsonb"`
	Action           ChangeCustomResourceAction
	CreatedAt        time.Time
}

func (ChangeCustomResource) TableName() string { return "change_custom_resources" }

type API struct {
	DB *gorm.DB
}

type ListFilter struct {
	Project   *string
	Cluster   *string
	Namespace *string
	Kind      *string
	Search    *string
}
