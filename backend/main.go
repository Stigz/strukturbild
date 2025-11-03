// backend/main.go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
)

func main() {
	if runningInLambda() {
		log.Printf("strukturbild backend starting in Lambda runtime (HTTP API v2)")
		// Defined in lambda_v2.go; starts lambda with the v2 handler.
		startLambdaV2()
		return
	}

	addr := serverAddress()
	log.Printf("strukturbild backend listening on %s", addr)
	if err := http.ListenAndServe(addr, newRouter()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func newRouter() *mux.Router {
	r := mux.NewRouter()
	r.Use(corsMiddleware)

	// Health (useful for local + parity with API check)
	r.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":"true"}`))
	}).Methods(http.MethodGet)
	r.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":"true"}`))
	}).Methods(http.MethodGet)

	// CORS preflight
	r.Methods(http.MethodOptions).HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-Amz-Date, X-Api-Key, X-Amz-Security-Token")
		w.Header().Set("Access-Control-Allow-Methods", "OPTIONS,GET,POST,DELETE,PATCH")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func serverAddress() string {
	if port := os.Getenv("PORT"); port != "" {
		if port[0] == ':' {
			return port
		}
		return ":" + port
	}
	return ":8080"
}

// runningInLambda reports whether we're executing inside the Lambda runtime.
func runningInLambda() bool {
	return os.Getenv("AWS_LAMBDA_RUNTIME_API") != ""
}
