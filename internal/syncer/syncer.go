package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	ksync "github.com/targc/ksync/pkg"
	"github.com/targc/ktrack"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type Syncer struct {
	APIURL       string
	APIToken     string
	IntervalSync time.Duration
	K8s          dynamic.Interface
	Mapper       meta.RESTMapper
	K8sConfig    *rest.Config
}

func (s *Syncer) Run(ctx context.Context) error {
	go s.runTracking(ctx)

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
		k8sErr = s.k8sApply(ctx, change.JSON, change.CustomResourceID)
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

func (s *Syncer) k8sApply(ctx context.Context, resource ksync.IResource, customResourceID uuid.UUID) error {
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(resource, &obj.Object); err != nil {
		return fmt.Errorf("failed to unmarshal resource: %w", err)
	}

	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["ksync/tracking"] = "true"
	labels["ksync/id"] = customResourceID.String()
	obj.SetLabels(labels)

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

// --- tracking ---

type trackGVR struct {
	APIVersion string `json:"api_version"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
}

type trackPhaseMapping struct {
	Kind   string `json:"kind"`
	Phase  string `json:"phase"`
	Status string `json:"status"`
}

type statusItem struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func (s *Syncer) runTracking(ctx context.Context) {
	for {
		if err := s.runTracker(ctx); err != nil {
			slog.Error("tracking cycle failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func (s *Syncer) runTracker(ctx context.Context) error {
	var gvrs []trackGVR
	if err := s.apiGet(ctx, "/api/v1/resources/gvrs", &gvrs); err != nil {
		return fmt.Errorf("failed to fetch GVRs: %w", err)
	}

	var rawMappings []trackPhaseMapping
	if err := s.apiGet(ctx, "/api/v1/resources/phase-mappings", &rawMappings); err != nil {
		return fmt.Errorf("failed to fetch phase mappings: %w", err)
	}

	mappings := make(map[string]map[string]string)
	for _, m := range rawMappings {
		k := strings.ToLower(m.Kind)
		p := strings.ToLower(m.Phase)

		if mappings[k] == nil {
			mappings[k] = make(map[string]string)
		}

		mappings[k][p] = m.Status
	}

	labels := map[string]string{"ksync/tracking": "true"}

	items := make([]ktrack.TrackItem, 0, len(gvrs))
	for _, g := range gvrs {
		apiGroup := ""
		if parts := strings.SplitN(g.APIVersion, "/", 2); len(parts) == 2 {
			apiGroup = parts[0]
		}

		items = append(items, ktrack.TrackItem{
			APIGroup:  apiGroup,
			Kind:      g.Kind,
			Namespace: g.Namespace,
			Labels:    labels,
		})

		slog.Info(
			"watching on > ",
			"api_group", apiGroup,
			"kind", g.Kind,
			"ns", g.Namespace,
			"labels", labels,
		)
	}

	if len(items) == 0 {
		return nil
	}

	tracker, err := ktrack.New(s.K8sConfig, s.IntervalSync, items)
	if err != nil {
		return fmt.Errorf("failed to create tracker: %w", err)
	}

	trackCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	var buf []statusItem

	tracker.Run(trackCtx, func(res ktrack.Resource) error { //nolint:errcheck

		slog.Info(
			"track polled > ",
			"api_group", res.APIGroup,
			"kind", res.Kind,
			"ns", res.Namespace,
			"labels", res.Labels,
			"name", res.Name,
		)

		id := res.Labels["ksync/id"]
		if id == "" {
			return nil
		}
		phase, _ := res.Status["phase"].(string)
		buf = append(buf, statusItem{ID: id, Status: mapPhaseStatus(res.Kind, phase, mappings)})
		return nil
	})

	for len(buf) > 0 {
		batch := buf
		if len(batch) > 100 {
			batch = buf[:100]
		}
		buf = buf[len(batch):]
		if err := s.apiPost(ctx, "/api/v1/resources/status", batch); err != nil {
			slog.Error("failed to flush status", "error", err)
		}
	}

	return nil
}

func mapPhaseStatus(kind, phase string, mappings map[string]map[string]string) string {
	if m, ok := mappings[strings.ToLower(kind)]; ok {
		if s, ok := m[strings.ToLower(phase)]; ok {
			return s
		}
	}
	return "pending"
}
