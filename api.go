package ksync

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

func (a *API) List(ctx context.Context, filter ListFilter, page, limit int64, dest *[]CustomResource) (int64, error) {
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

func (a *API) Get(ctx context.Context, id uuid.UUID, dest *CustomResource) error {
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

func parseManifest(jsn IResource) (apiVersion, kind, namespace, name string) {
	var manifest struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"metadata"`
	}
	json.Unmarshal(jsn, &manifest) //nolint:errcheck
	return manifest.APIVersion, manifest.Kind, manifest.Metadata.Namespace, manifest.Metadata.Name
}

func (a *API) Create(ctx context.Context, customResourceID uuid.UUID, cluster string, jsn IResource) error {
	apiVersion, kind, namespace, name := parseManifest(jsn)

	cr := &CustomResource{
		ID:         customResourceID,
		Cluster:    cluster,
		APIVersion: apiVersion,
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
	}

	err := a.DB.
		WithContext(ctx).
		Create(cr).
		Error

	if err != nil {
		return fmt.Errorf("failed to create custom resource: %w", err)
	}

	change := &ChangeCustomResource{
		ID:               uuid.New(),
		CustomResourceID: customResourceID,
		JSON:             jsn,
		Action:           ChangeCustomResourceActionApply,
	}

	err = a.DB.
		WithContext(ctx).
		Create(change).
		Error

	if err != nil {
		return fmt.Errorf("failed to create change: %w", err)
	}

	return nil
}

func (a *API) Update(ctx context.Context, customResourceID uuid.UUID, jsn IResource) error {
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

func (a *API) Remove(ctx context.Context, id uuid.UUID) error {
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
