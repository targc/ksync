package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	ksync "github.com/targc/ksync/pkg"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type Syncer struct {
	APIURL       string
	APIToken     string
	IntervalSync time.Duration
	K8s          dynamic.Interface
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
	var changes []ksync.SyncChange
	if err := s.apiGet(ctx, "/api/v1/changes", &changes); err != nil {
		return
	}

	for _, change := range changes {
		s.applyChange(ctx, change)
	}
}

func (s *Syncer) applyChange(ctx context.Context, change ksync.SyncChange) {
	if err := s.apiPost(ctx, fmt.Sprintf("/api/v1/changes/%s/syncing", change.ID), nil); err != nil {
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
		s.apiPost(ctx, fmt.Sprintf("/api/v1/changes/%s/error", change.ID), map[string]string{"error": k8sErr.Error()}) //nolint:errcheck
		return
	}

	s.apiPost(ctx, fmt.Sprintf("/api/v1/changes/%s/success", change.ID), nil) //nolint:errcheck
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

	gvr, ns, name, err := parseGVR(obj)
	if err != nil {
		return err
	}

	_, err = s.K8s.Resource(gvr).Namespace(ns).Apply(ctx, name, obj, metav1.ApplyOptions{
		FieldManager: "ksync",
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
