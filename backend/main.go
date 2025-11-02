package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
)

func main() {
	if err := ensureLoaded(); err != nil {
		log.Fatalf("failed to load fixtures: %v", err)
	}

	router := newRouter()
	addr := serverAddress()
	log.Printf("strukturbild backend listening on %s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func newRouter() *mux.Router {
	router := mux.NewRouter()
	router.Use(corsMiddleware)

	router.HandleFunc("/api/stories", ListStories).Methods(http.MethodGet)
	router.HandleFunc("/api/stories/{id}/full", GetStoryFull).Methods(http.MethodGet)
	router.HandleFunc("/api/stories/import", ImportStory).Methods(http.MethodPost)
	router.HandleFunc("/struktur/{storyId}", GetStrukturByStory).Methods(http.MethodGet)
	router.HandleFunc("/submit", SubmitHandler).Methods(http.MethodPost)
	router.HandleFunc("/struktur/{storyId}/{nodeId}", DeleteNode).Methods(http.MethodDelete)

	router.Methods(http.MethodOptions).HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return router
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "OPTIONS,GET,POST,DELETE")
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
