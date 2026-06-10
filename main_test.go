package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func TestWebhookOperationHandler(t *testing.T) {
	tests := []struct {
		name            string
		operation       admissionv1.Operation
		expectedStatus  int
		expectedAllowed bool
	}{
		{"CREATE", admissionv1.Create, http.StatusOK, true},
		{"DELETE", admissionv1.Delete, http.StatusOK, true},
		{"CONNECT", admissionv1.Connect, http.StatusOK, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "admission.k8s.io/v1",
					Kind:       "AdmissionReview",
				},
				Request: &admissionv1.AdmissionRequest{
					UID:       "uuid",
					Kind:      metav1.GroupVersionKind{Kind: "Application"},
					Operation: tt.operation,
					OldObject: runtime.RawExtension{Raw: []byte(`{"metadata": {}, "spec": {}, "status": {}}`)},
					Object:    runtime.RawExtension{Raw: []byte(`{"metadata": {}, "spec": {}, "status": {}}`)},
				},
			}

			reqBytes, err := json.Marshal(reqBody)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(reqBytes))
			w := httptest.NewRecorder()

			handleAdmissionReview(w, req)

			resp := w.Result()
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status code 200, got %d", resp.StatusCode)
			}

			var admissionResp admissionv1.AdmissionReview
			if err := json.NewDecoder(resp.Body).Decode(&admissionResp); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if admissionResp.Response == nil {
				t.Fatalf("Expected a response, got nil")
			}

			if admissionResp.Response.UID != reqBody.Request.UID {
				t.Errorf("Expected UID %s, got %s", reqBody.Request.UID, admissionResp.Response.UID)
			}

			if !admissionResp.Response.Allowed {
				t.Errorf("Expected response to be allowed, but it was denied")
			}
		})
	}
}

func TestHandleAdmissionReview_StatusSyncRevisionChange(t *testing.T) {
	reqBody := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Request: &admissionv1.AdmissionRequest{
			UID:       "test-uid-status-sync-revision-change",
			Kind:      metav1.GroupVersionKind{Kind: "Application"},
			Operation: admissionv1.Update,
			OldObject: runtime.RawExtension{Raw: []byte(`{"metadata": {}, "spec": {}, "status": {"sync": {"revision": "abc123"}}}`)},
			Object:    runtime.RawExtension{Raw: []byte(`{"metadata": {}, "spec": {}, "status": {"sync": {"revision": "def456"}}}`)},
		},
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(reqBytes))
	w := httptest.NewRecorder()

	handleAdmissionReview(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", resp.StatusCode)
	}

	var admissionResp admissionv1.AdmissionReview
	if err := json.NewDecoder(resp.Body).Decode(&admissionResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if admissionResp.Response == nil {
		t.Fatalf("Expected a response, got nil")
	}

	if admissionResp.Response.UID != reqBody.Request.UID {
		t.Errorf("Expected UID %s, got %s", reqBody.Request.UID, admissionResp.Response.UID)
	}

	if !admissionResp.Response.Allowed {
		t.Errorf("Expected response to be allowed, but it was denied")
	}
}

// doAdmissionReview marshals the request, runs the handler, and returns the
// HTTP recorder so individual tests can assert on status code and response body.
func doAdmissionReview(t *testing.T, reqBody admissionv1.AdmissionReview) *httptest.ResponseRecorder {
	t.Helper()

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(reqBytes))
	w := httptest.NewRecorder()
	handleAdmissionReview(w, req)
	return w
}

// updateReview builds an UPDATE AdmissionReview for an Application from the raw
// old and new object JSON.
func updateReview(uid string, oldRaw, newRaw string) admissionv1.AdmissionReview {
	return admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Request: &admissionv1.AdmissionRequest{
			UID:       types.UID(uid),
			Kind:      metav1.GroupVersionKind{Kind: "Application"},
			Operation: admissionv1.Update,
			OldObject: runtime.RawExtension{Raw: []byte(oldRaw)},
			Object:    runtime.RawExtension{Raw: []byte(newRaw)},
		},
	}
}

func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) *admissionv1.AdmissionReview {
	t.Helper()

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status code 200, got %d", resp.StatusCode)
	}

	var admissionResp admissionv1.AdmissionReview
	if err := json.NewDecoder(resp.Body).Decode(&admissionResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if admissionResp.Response == nil {
		t.Fatalf("Expected a response, got nil")
	}
	return &admissionResp
}

// TestHandleAdmissionReview_NoSignificantChange covers the core dedup behavior:
// when old and new objects are identical, the update must be denied.
func TestHandleAdmissionReview_NoSignificantChange(t *testing.T) {
	obj := `{"metadata": {"name": "app"}, "spec": {"project": "default"}, "status": {"sync": {"status": "Synced"}}}`
	resp := decodeResponse(t, doAdmissionReview(t, updateReview("no-change", obj, obj)))

	if resp.Response.Allowed {
		t.Errorf("Expected update to be denied for an identical object, but it was allowed")
	}
}

// TestHandleAdmissionReview_OnlyReconciledAtIgnoredFields verifies that updates
// touching only reconciledAt, managedFields, and generation are treated as no-ops.
func TestHandleAdmissionReview_OnlyReconciledAtIgnoredFields(t *testing.T) {
	oldRaw := `{"metadata": {"name": "app", "generation": 1, "managedFields": [{"manager": "a"}]}, "spec": {}, "status": {"reconciledAt": "2024-03-20T12:00:00Z"}}`
	newRaw := `{"metadata": {"name": "app", "generation": 2, "managedFields": [{"manager": "b"}]}, "spec": {}, "status": {"reconciledAt": "2024-03-21T12:00:00Z"}}`
	resp := decodeResponse(t, doAdmissionReview(t, updateReview("ignored-fields", oldRaw, newRaw)))

	if resp.Response.Allowed {
		t.Errorf("Expected update to be denied when only ignored fields changed, but it was allowed")
	}
}

// TestHandleAdmissionReview_SpecChange verifies a real spec change is allowed.
func TestHandleAdmissionReview_SpecChange(t *testing.T) {
	oldRaw := `{"metadata": {}, "spec": {"project": "default"}, "status": {}}`
	newRaw := `{"metadata": {}, "spec": {"project": "production"}, "status": {}}`
	resp := decodeResponse(t, doAdmissionReview(t, updateReview("spec-change", oldRaw, newRaw)))

	if !resp.Response.Allowed {
		t.Errorf("Expected update to be allowed for a spec change, but it was denied")
	}
}

// TestHandleAdmissionReview_NonApplicationKind verifies non-Application kinds
// pass through untouched.
func TestHandleAdmissionReview_NonApplicationKind(t *testing.T) {
	review := updateReview("other-kind",
		`{"metadata": {}, "spec": {}, "status": {}}`,
		`{"metadata": {}, "spec": {}, "status": {}}`)
	review.Request.Kind = metav1.GroupVersionKind{Kind: "ConfigMap"}
	resp := decodeResponse(t, doAdmissionReview(t, review))

	if !resp.Response.Allowed {
		t.Errorf("Expected non-Application update to be allowed, but it was denied")
	}
}

// TestHandleAdmissionReview_NilRequest verifies a payload without an admission
// request is rejected instead of panicking.
func TestHandleAdmissionReview_NilRequest(t *testing.T) {
	w := doAdmissionReview(t, admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status code 400 for a nil request, got %d", w.Code)
	}
}

// TestHandleAdmissionReview_MethodNotAllowed verifies non-POST methods are rejected.
func TestHandleAdmissionReview_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/validate", nil)
	w := httptest.NewRecorder()
	handleAdmissionReview(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status code 405 for a GET request, got %d", w.Code)
	}
}

// TestHandleHealth verifies the health endpoint returns 200 OK.
func TestHandleHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200 from health endpoint, got %d", w.Code)
	}
}

func TestHandleAdmissionReview_StatusReconciledAtChange(t *testing.T) {
	reqBody := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Request: &admissionv1.AdmissionRequest{
			UID:       "test-uid-status-change",
			Kind:      metav1.GroupVersionKind{Kind: "Application"},
			Operation: admissionv1.Update,
			OldObject: runtime.RawExtension{Raw: []byte(`{"metadata": {}, "spec": {}, "status": {"reconciledAt": "2024-03-20T12:00:00Z"}}`)},
			Object:    runtime.RawExtension{Raw: []byte(`{"metadata": {}, "spec": {}, "status": {"reconciledAt": "2024-03-21T12:00:00Z"}}`)},
		},
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(reqBytes))
	w := httptest.NewRecorder()

	handleAdmissionReview(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", resp.StatusCode)
	}

	var admissionResp admissionv1.AdmissionReview
	if err := json.NewDecoder(resp.Body).Decode(&admissionResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if admissionResp.Response == nil {
		t.Fatalf("Expected a response, got nil")
	}

	if admissionResp.Response.UID != reqBody.Request.UID {
		t.Errorf("Expected UID %s, got %s", reqBody.Request.UID, admissionResp.Response.UID)
	}

	if admissionResp.Response.Allowed {
		t.Errorf("Expected response to be denied, but it was allowed")
	}
}
