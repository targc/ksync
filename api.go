package onelink

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

func (a *API) List(ctx context.Context, filter ListFilter, page, limit int64, dest *[]CustomResource) (int64, error) {
	q := a.DB.WithContext(ctx).Model(&CustomResource{})

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
		First(dest, "id = ?", id).
		Error

	if err != nil {
		return fmt.Errorf("failed to get custom resource: %w", err)
	}

	return nil
}

func (a *API) Apply(ctx context.Context, customResourceID uuid.UUID, jsn IResource) error {
	var manifest struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"metadata"`
	}
	json.Unmarshal(jsn, &manifest) //nolint:errcheck

	cr := &CustomResource{
		ID:         customResourceID,
		APIVersion: manifest.APIVersion,
		Kind:       manifest.Kind,
		Namespace:  manifest.Metadata.Namespace,
		Name:       manifest.Metadata.Name,
	}

	err := a.DB.
		WithContext(ctx).
		Where("id = ?", customResourceID).
		Assign(cr).
		FirstOrCreate(cr).
		Error

	if err != nil {
		return fmt.Errorf("failed to upsert custom resource: %w", err)
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
