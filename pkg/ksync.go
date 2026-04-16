package ksync

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type IResource = json.RawMessage

type ApiToken struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	Token     string
	Cluster   string
	CreatedAt time.Time
}

func (ApiToken) TableName() string { return "ksync_api_tokens" }

// SyncChange is returned by GET /changes — extends ChangeCustomResource with CR identity fields needed for delete.
type SyncChange struct {
	ChangeCustomResource
	CRAPIVersion string `gorm:"column:cr_api_version" json:"cr_api_version"`
	CRKind       string `gorm:"column:cr_kind"        json:"cr_kind"`
	CRNamespace  string `gorm:"column:cr_namespace"   json:"cr_namespace"`
	CRName       string `gorm:"column:cr_name"        json:"cr_name"`
}

type CustomResource struct {
	ID                            uuid.UUID `gorm:"type:uuid;primaryKey"`
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

func (CustomResource) TableName() string { return "ksync_custom_resources" }

type ChangeCustomResourceAction string

var (
	ChangeCustomResourceActionApply  ChangeCustomResourceAction = "apply"
	ChangeCustomResourceActionDelete ChangeCustomResourceAction = "delete"
)

type ChangeCustomResource struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey"`
	CustomResourceID uuid.UUID `gorm:"type:uuid;index"`
	JSON             IResource `gorm:"type:jsonb"`
	Action           ChangeCustomResourceAction
	CreatedAt        time.Time
}

func (ChangeCustomResource) TableName() string { return "ksync_change_custom_resources" }
