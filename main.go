package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func handleAdmissionReview(w http.ResponseWriter, r *http.Request) {
	var admissionReviewReq admissionv1.AdmissionReview
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusInternalServerError)
		return
	}

	err = json.Unmarshal(body, &admissionReviewReq)
	if err != nil {
		http.Error(w, "failed to unmarshal request", http.StatusBadRequest)
		return
	}

	// Default AdmissionReview response
	admissionReviewResp := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:     admissionReviewReq.Request.UID,
			Allowed: true,
		},
	}

	// Only process UPDATE requests for Application CR
	if admissionReviewReq.Request.Operation != admissionv1.Update || admissionReviewReq.Request.Kind.Kind != "Application" {
		sendResponse(w, admissionReviewResp)
		return
	}

	// Parse old and new objects
	var oldObj, newObj map[string]interface{}
	err = json.Unmarshal(admissionReviewReq.Request.OldObject.Raw, &oldObj)
	if err != nil {
		http.Error(w, "failed to parse old object", http.StatusInternalServerError)
		return
	}

	err = json.Unmarshal(admissionReviewReq.Request.Object.Raw, &newObj)
	if err != nil {
		http.Error(w, "failed to parse new object", http.StatusInternalServerError)
		return
	}

	// Remove reconciledAt from both old and new objects
	removeReconciledAt(oldObj)
	removeReconciledAt(newObj)

	// Compare only spec and status (ignoring metadata)
	oldSpec := oldObj["spec"]
	newSpec := newObj["spec"]
	oldStatus := oldObj["status"]
	newStatus := newObj["status"]

	// Create new objects with just spec and status for comparison
	oldFilteredObj := map[string]interface{}{"spec": oldSpec, "status": oldStatus}
	newFilteredObj := map[string]interface{}{"spec": newSpec, "status": newStatus}

	// Compare old and new filtered objects using reflect.DeepEqual
	if reflect.DeepEqual(oldFilteredObj, newFilteredObj) {
		// If they are the same after removing reconciledAt, return success to the client
		fmt.Printf("No differences found between old and new spec/status.\n")

		admissionReviewResp.Response.Allowed = false
		admissionReviewResp.Response.Result = &metav1.Status{
			Status:  "Success",
			Message: "Update successful.",
			Code:    http.StatusOK, // HTTP 200
		}
	} else {
		// If there are any differences, log them
		fmt.Println("Differences found between old and new spec/status:")

		// Log the differences in spec and status
		printDifferences(oldFilteredObj, newFilteredObj)

		// Allow the update to pass
		admissionReviewResp.Response.Allowed = true
	}

	sendResponse(w, admissionReviewResp)
}

// Helper function to remove reconciledAt from an object
func removeReconciledAt(obj map[string]interface{}) {
	if status, exists := obj["status"].(map[string]interface{}); exists {
		delete(status, "reconciledAt")
	}
}

func sendResponse(w http.ResponseWriter, admissionReviewResp admissionv1.AdmissionReview) {
	responseBytes, _ := json.Marshal(admissionReviewResp)
	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}

// Helper function to print the differences between old and new objects
func printDifferences(oldObj, newObj map[string]interface{}) {
	// Check the difference in keys and values
	for key, oldValue := range oldObj {
		if newValue, exists := newObj[key]; exists {
			if !reflect.DeepEqual(oldValue, newValue) {
				// Log the differences
				fmt.Printf("Field: %s\n", key)
				fmt.Printf("Old Value: %v\n", oldValue)
				fmt.Printf("New Value: %v\n\n", newValue)
			}
		} else {
			// If the key is missing in the new object
			fmt.Printf("Field: %s\n", key)
			fmt.Printf("Old Value: %v\n", oldValue)
			fmt.Println("New Value: null")
		}
	}

	// Check if there are any new fields in the new object
	for key, newValue := range newObj {
		if _, exists := oldObj[key]; !exists {
			// If the key is missing in the old object
			fmt.Printf("Field: %s\n", key)
			fmt.Println("Old Value: null")
			fmt.Printf("New Value: %v\n\n", newValue)
		}
	}
}

func main() {
	http.HandleFunc("/validate", handleAdmissionReview)
	fmt.Println("Starting webhook server on :8443...")
	err := http.ListenAndServeTLS(":8443", "/certs/tls.crt", "/certs/tls.key", nil)
	if err != nil {
		fmt.Println("Failed to start webhook server:", err)
	}
}
