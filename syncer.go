package onelink

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (s *Syncer) Run(ctx context.Context) error {
	for {
		s.sync(ctx)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.IntervalSync):
		}
	}
}

func (s *Syncer) sync(ctx context.Context) {
	var changes []ChangeCustomResource
	err := s.DB.
		WithContext(ctx).
		Raw(`
			SELECT DISTINCT ON (c.custom_resource_id) c.*
			FROM change_custom_resources c
			JOIN custom_resources r ON r.id = c.custom_resource_id
			WHERE r.cluster = ?
			ORDER BY c.custom_resource_id, c.id
			LIMIT 100
		`, s.Cluster).
		Scan(&changes).
		Error

	if err != nil {
		return
	}

	for _, change := range changes {
		if err := s.applyChange(ctx, change); err != nil {
			continue
		}
	}
}

func (s *Syncer) applyChange(ctx context.Context, change ChangeCustomResource) error {
	err := s.DB.
		WithContext(ctx).
		Model(&CustomResource{}).
		Where("id = ?", change.CustomResourceID).
		Update("syncing_change_custom_resource_id", change.ID).
		Error

	if err != nil {
		return fmt.Errorf("failed to lock custom resource: %w", err)
	}

	var k8sErr error
	switch change.Action {
	case ChangeCustomResourceActionApply:
		k8sErr = s.k8sApply(ctx, change.JSON)
	case ChangeCustomResourceActionDelete:
		var cr CustomResource
		err := s.DB.
			WithContext(ctx).
			First(&cr, "id = ?", change.CustomResourceID).
			Error

		if err != nil {
			k8sErr = fmt.Errorf("failed to get custom resource: %w", err)
			break
		}

		k8sErr = s.k8sDelete(ctx, cr.APIVersion, cr.Kind, cr.Namespace, cr.Name)
	}

	if k8sErr != nil {
		err = s.DB.
			WithContext(ctx).
			Model(&CustomResource{}).
			Where("id = ?", change.CustomResourceID).
			Updates(map[string]interface{}{
				"syncing_change_custom_resource_id": nil,
				"last_sync_error":                   k8sErr.Error(),
			}).
			Error

		if err != nil {
			return fmt.Errorf("failed to update sync error: %w", err)
		}

		return k8sErr
	}

	err = s.DB.
		WithContext(ctx).
		Delete(&ChangeCustomResource{}, "id = ?", change.ID).
		Error

	if err != nil {
		return fmt.Errorf("failed to delete change: %w", err)
	}

	updates := map[string]interface{}{
		"syncing_change_custom_resource_id": nil,
		"last_change_custom_resource_id":    change.ID,
		"last_sync_error":                   nil,
	}
	if change.Action == ChangeCustomResourceActionApply {
		updates["json"] = change.JSON
	}

	err = s.DB.
		WithContext(ctx).
		Model(&CustomResource{}).
		Where("id = ?", change.CustomResourceID).
		Updates(updates).
		Error

	if err != nil {
		return fmt.Errorf("failed to update custom resource: %w", err)
	}

	return nil
}

func (s *Syncer) k8sApply(ctx context.Context, resource IResource) error {
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(resource, &obj.Object); err != nil {
		return fmt.Errorf("failed to unmarshal resource: %w", err)
	}

	gvr, ns, name, err := parseGVR(obj)
	if err != nil {
		return err
	}

	_, err = s.K8s.Resource(gvr).Namespace(ns).Apply(ctx, name, obj, metav1.ApplyOptions{
		FieldManager: "onelink",
		Force:        true,
	})
	if err != nil {
		return fmt.Errorf("failed to apply resource: %w", err)
	}
	return nil
}

func (s *Syncer) k8sDelete(ctx context.Context, apiVersion, kind, namespace, name string) error {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return fmt.Errorf("failed to parse apiVersion: %w", err)
	}

	gvr := gv.WithResource(strings.ToLower(kind) + "s")

	err = s.K8s.Resource(gvr).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})

	if err != nil {
		return fmt.Errorf("failed to delete resource: %w", err)
	}

	return nil
}

func parseGVR(obj *unstructured.Unstructured) (schema.GroupVersionResource, string, string, error) {
	gv, err := schema.ParseGroupVersion(obj.GetAPIVersion())
	if err != nil {
		return schema.GroupVersionResource{}, "", "", fmt.Errorf("failed to parse apiVersion: %w", err)
	}
	resource := strings.ToLower(obj.GetKind()) + "s"
	return gv.WithResource(resource), obj.GetNamespace(), obj.GetName(), nil
}
