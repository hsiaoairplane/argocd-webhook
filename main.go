package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	log "github.com/sirupsen/logrus"
)

func init() {
	log.SetFormatter(&log.JSONFormatter{})
	log.SetOutput(os.Stdout)
}

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
			UID: admissionReviewReq.Request.UID,
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

	// Remove metadata.managedFields and metadata.generation
	cleanupMetadata(oldObj)
	cleanupMetadata(newObj)

	// Remove reconciledAt from both old and new objects
	removeReconciledAt(oldObj)
	removeReconciledAt(newObj)

	metadataChanged := !reflect.DeepEqual(oldObj["metadata"], newObj["metadata"])
	specChanged := !reflect.DeepEqual(oldObj["spec"], newObj["spec"])
	statusChanged := !reflect.DeepEqual(oldObj["status"], newObj["status"])

	if !metadataChanged && !specChanged && !statusChanged {
		log.Info("No significant differences found.")

		admissionReviewResp.Response.Allowed = false
		admissionReviewResp.Response.Result = &metav1.Status{
			Status:  "Success",
			Message: "Update successful.",
			Code:    http.StatusOK,
		}
	} else {
		if metadataChanged {
			printMetadataDifferences(oldObj, newObj)
		}
		if specChanged {
			printSpecDifferences(oldObj, newObj)
		}
		if statusChanged {
			printStatusDifferences(oldObj, newObj)
		}
		admissionReviewResp.Response.Allowed = true
	}

	sendResponse(w, admissionReviewResp)
}

// Function to remove metadata.managedFields and metadata.generation
func cleanupMetadata(obj map[string]interface{}) {
	if metadata, exists := obj["metadata"].(map[string]interface{}); exists {
		delete(metadata, "managedFields")
		delete(metadata, "generation")
	}
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

// Function to log metadata differences
func printMetadataDifferences(oldObj, newObj map[string]interface{}) {
	oldMetadata, _ := oldObj["metadata"].(map[string]interface{})
	newMetadata, _ := newObj["metadata"].(map[string]interface{})
	printDifferences("Metadata", oldMetadata, newMetadata)
}

// Function to log spec differences
func printSpecDifferences(oldObj, newObj map[string]interface{}) {
	oldSpec, _ := oldObj["spec"].(map[string]interface{})
	newSpec, _ := newObj["spec"].(map[string]interface{})
	printDifferences("Spec", oldSpec, newSpec)
}

// Function to log status differences
func printStatusDifferences(oldObj, newObj map[string]interface{}) {
	oldStatus, _ := oldObj["status"].(map[string]interface{})
	newStatus, _ := newObj["status"].(map[string]interface{})
	printDifferences("Status", oldStatus, newStatus)
}

// Function to print differences between two objects
func printDifferences(owner string, oldMap, newMap map[string]interface{}) {
	if oldMap == nil && newMap == nil {
		return
	}

	log.Debug("----- ", owner, " Differences -----")

	for key, oldValue := range oldMap {
		if newValue, exists := newMap[key]; exists {
			if !reflect.DeepEqual(oldValue, newValue) {
				log.Debugf("Key: %s\n  Old Value: %v\n  New Value: %v\n", key, oldValue, newValue)
			}
		} else {
			log.Debugf("Key removed: %s (Old Value: %v)", key, oldValue)
		}
	}

	for key, newValue := range newMap {
		if _, exists := oldMap[key]; !exists {
			log.Debugf("Key added: %s (New Value: %v)", key, newValue)
		}
	}
}

func main() {
	port := flag.String("port", "8443", "Webhook server port")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error, fatal, panic)")
	flag.Parse()

	addr := fmt.Sprintf(":%s", *port)
	srv := &http.Server{
		Addr:    addr,
		Handler: http.DefaultServeMux,
	}

	level, err := log.ParseLevel(*logLevel)
	if err != nil {
		log.Fatalf("Invalid log level: %s", *logLevel)
	}
	log.SetLevel(level)

	http.HandleFunc("/validate", handleAdmissionReview)
	log.Info("Starting webhook server on :8443...")

	go func() {
		if err := srv.ListenAndServeTLS("/certs/tls.crt", "/certs/tls.key"); err != nil && err != http.ErrServerClosed {
			log.Fatal("Failed to start webhook server:", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Info("Server exiting")
}
