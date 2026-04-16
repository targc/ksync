package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	ksync "github.com/targc/ksync/pkg"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type Syncer struct {
	APIURL       string
	APIToken     string
	IntervalSync time.Duration
	K8s          dynamic.Interface
	Mapper       meta.RESTMapper
}

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

	slog.Info("poll syncing...")

	var changes []ksync.SyncChange
	if err := s.apiGet(ctx, "/api/v1/changes", &changes); err != nil {
		slog.Error("failed to fetch changes", "error", err)
		return
	}

	if len(changes) > 0 {
		slog.Info("syncing", "changes", len(changes))
	}

	for _, change := range changes {
		s.applyChange(ctx, change)
	}
}

func (s *Syncer) applyChange(ctx context.Context, change ksync.SyncChange) {
	log := slog.With("change_id", change.ID, "action", change.Action, "kind", change.CRKind, "name", change.CRName, "namespace", change.CRNamespace)

	if err := s.apiPost(ctx, fmt.Sprintf("/api/v1/changes/%s/syncing", change.ID), nil); err != nil {
		log.Error("failed to set syncing", "error", err)
		return
	}

	var k8sErr error
	switch change.Action {
	case ksync.ChangeCustomResourceActionApply:
		k8sErr = s.k8sApply(ctx, change.JSON)
	case ksync.ChangeCustomResourceActionDelete:
		k8sErr = s.k8sDelete(ctx, change.CRAPIVersion, change.CRKind, change.CRNamespace, change.CRName)
	}

	if k8sErr != nil {
		log.Error("k8s apply failed", "error", k8sErr)
		s.apiPost(ctx, fmt.Sprintf("/api/v1/changes/%s/error", change.ID), map[string]string{"error": k8sErr.Error()}) //nolint:errcheck
		return
	}

	if err := s.apiPost(ctx, fmt.Sprintf("/api/v1/changes/%s/success", change.ID), nil); err != nil {
		log.Error("failed to report success", "error", err)
		return
	}

	log.Info("applied")
}

func (s *Syncer) apiGet(ctx context.Context, path string, dest interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.APIURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.APIToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned %d", path, resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(dest)
}

func (s *Syncer) apiPost(ctx context.Context, path string, body interface{}) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.APIURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.APIToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %s returned %d", path, resp.StatusCode)
	}

	return nil
}

func (s *Syncer) k8sApply(ctx context.Context, resource ksync.IResource) error {
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(resource, &obj.Object); err != nil {
		return fmt.Errorf("failed to unmarshal resource: %w", err)
	}

	gvr, err := s.resolveGVR(obj.GetAPIVersion(), obj.GetKind())
	if err != nil {
		return err
	}

	_, err = s.K8s.Resource(gvr).Namespace(obj.GetNamespace()).Apply(ctx, obj.GetName(), obj, metav1.ApplyOptions{
		FieldManager: "ksync",
		Force:        true,
	})
	if err != nil {
		return fmt.Errorf("failed to apply resource: %w", err)
	}
	return nil
}

func (s *Syncer) k8sDelete(ctx context.Context, apiVersion, kind, namespace, name string) error {
	gvr, err := s.resolveGVR(apiVersion, kind)
	if err != nil {
		return err
	}

	err = s.K8s.Resource(gvr).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete resource: %w", err)
	}

	return nil
}

func (s *Syncer) resolveGVR(apiVersion, kind string) (schema.GroupVersionResource, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to parse apiVersion: %w", err)
	}

	mapping, err := s.Mapper.RESTMapping(schema.GroupKind{Group: gv.Group, Kind: kind}, gv.Version)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to get REST mapping for %s/%s: %w", apiVersion, kind, err)
	}

	return mapping.Resource, nil
}
