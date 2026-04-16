package ksync

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ListFilter struct {
	Project   *string
	Cluster   *string
	Namespace *string
	Kind      *string
	Search    *string
}

type SDK struct {
	DB *gorm.DB
}

func (a *SDK) List(ctx context.Context, filter ListFilter, page, limit int64, dest *[]CustomResource) (int64, error) {
	q := a.DB.WithContext(ctx).Model(&CustomResource{}).Where("deleted_at IS NULL")

	if filter.Project != nil {
		q = q.Where("project = ?", *filter.Project)
	}
	if filter.Cluster != nil {
		q = q.Where("cluster = ?", *filter.Cluster)
	}
	if filter.Namespace != nil {
		q = q.Where("namespace = ?", *filter.Namespace)
	}
	if filter.Kind != nil {
		q = q.Where("kind = ?", *filter.Kind)
	}
	if filter.Search != nil {
		q = q.Where("name ILIKE ?", "%"+*filter.Search+"%")
	}

	var total int64
	err := q.Count(&total).Error

	if err != nil {
		return 0, fmt.Errorf("failed to count custom resources: %w", err)
	}

	offset := (page - 1) * limit
	err = q.
		Offset(int(offset)).
		Limit(int(limit)).
		Find(dest).
		Error

	if err != nil {
		return 0, fmt.Errorf("failed to list custom resources: %w", err)
	}

	return total, nil
}

func (a *SDK) Get(ctx context.Context, id uuid.UUID, dest *CustomResource) error {
	err := a.DB.
		WithContext(ctx).
		Where("deleted_at IS NULL").
		First(dest, "id = ?", id).
		Error

	if err != nil {
		return fmt.Errorf("failed to get custom resource: %w", err)
	}

	return nil
}

func parseManifest(jsn IResource) (apiVersion, kind, namespace, name string, err error) {
	var manifest struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(jsn, &manifest); err != nil {
		return "", "", "", "", fmt.Errorf("failed to parse manifest: %w", err)
	}
	return manifest.APIVersion, manifest.Kind, manifest.Metadata.Namespace, manifest.Metadata.Name, nil
}

func (a *SDK) Create(ctx context.Context, customResourceID uuid.UUID, project, cluster string, jsn IResource) error {
	apiVersion, kind, namespace, name, err := parseManifest(jsn)
	if err != nil {
		return err
	}

	tx := a.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	cr := &CustomResource{
		ID:         customResourceID,
		Project:    project,
		Cluster:    cluster,
		APIVersion: apiVersion,
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
	}

	if err := tx.Create(cr).Error; err != nil {
		return fmt.Errorf("failed to create custom resource: %w", err)
	}

	change := &ChangeCustomResource{
		ID:               uuid.New(),
		CustomResourceID: customResourceID,
		JSON:             jsn,
		Action:           ChangeCustomResourceActionApply,
	}

	if err := tx.Create(change).Error; err != nil {
		return fmt.Errorf("failed to create change: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}

func (a *SDK) Update(ctx context.Context, customResourceID uuid.UUID, jsn IResource) error {
	change := &ChangeCustomResource{
		ID:               uuid.New(),
		CustomResourceID: customResourceID,
		JSON:             jsn,
		Action:           ChangeCustomResourceActionApply,
	}

	err := a.DB.
		WithContext(ctx).
		Create(change).
		Error

	if err != nil {
		return fmt.Errorf("failed to create change: %w", err)
	}

	return nil
}

func (a *SDK) Remove(ctx context.Context, id uuid.UUID) error {
	change := &ChangeCustomResource{
		ID:               uuid.New(),
		CustomResourceID: id,
		Action:           ChangeCustomResourceActionDelete,
	}

	err := a.DB.
		WithContext(ctx).
		Create(change).
		Error

	if err != nil {
		return fmt.Errorf("failed to create delete change: %w", err)
	}

	return nil
}
