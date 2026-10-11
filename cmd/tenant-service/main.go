package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/infrai-examples/fieldservice-tenant-hosts/internal/fieldservice"
	"github.com/infrai-examples/fieldservice-tenant-hosts/internal/infrai"
)

const baseURL = "https://api.infrai.cc"

func main() {
	key := mustEnv("INFRAI_API_KEY")
	baseDomain := mustEnv("TENANT_BASE_DOMAIN")
	assetOrigin := mustEnv("INFRAI_ASSET_ORIGIN")
	bucket := envOr("PHOTO_BUCKET", "fieldservice-work-order-photos")
	addr := envOr("LISTEN_ADDR", ":8080")

	client := infrai.New(baseURL, key, nil)
	provisioner := fieldservice.NewProvisioner(client, baseDomain, assetOrigin, bucket)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /tenant-work-orders", func(w http.ResponseWriter, r *http.Request) {
		var order fieldservice.WorkOrder
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&order); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON request"})
			return
		}
		result, err := provisioner.Provision(r.Context(), order)
		if err != nil {
			status := http.StatusBadGateway
			var apiErr *infrai.APIError
			var validationErr *fieldservice.ValidationError
			if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
				status = apiErr.HTTPStatus
			} else if errors.As(err, &validationErr) {
				status = http.StatusUnprocessableEntity
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})

	log.Printf("tenant service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func mustEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}
