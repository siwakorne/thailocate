// Command thailocate-server exposes the thailocate library over HTTP.
//
//	go run ./cmd/thailocate-server
//	curl "http://localhost:8080/v1/locate?lat=13.7563&lng=100.5018"
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/siwakorne/thailocate"
)

//go:embed web
var webFS embed.FS

func main() {
	addr := flag.String("addr", "", "listen address (defaults to $PORT if set, else :8080)")
	flag.Parse()

	listenAddr := *addr
	if listenAddr == "" {
		if port := os.Getenv("PORT"); port != "" {
			listenAddr = ":" + port
		} else {
			listenAddr = ":8080"
		}
	}

	loc, err := thailocate.New()
	if err != nil {
		log.Fatalf("failed to load boundary data: %v", err)
	}
	log.Println("boundary data loaded, starting server on", listenAddr)

	mux := http.NewServeMux()

	webRoot, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("failed to load embedded web assets: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(webRoot)))

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("/v1/locate", func(w http.ResponseWriter, r *http.Request) {
		writeJSON := func(status int, body any) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(body)
		}

		latStr := r.URL.Query().Get("lat")
		lngStr := r.URL.Query().Get("lng")
		if latStr == "" || lngStr == "" {
			writeJSON(http.StatusBadRequest, map[string]string{
				"error": "both 'lat' and 'lng' query parameters are required",
			})
			return
		}

		lat, err := strconv.ParseFloat(latStr, 64)
		if err != nil {
			writeJSON(http.StatusBadRequest, map[string]string{"error": "invalid 'lat': " + err.Error()})
			return
		}
		lng, err := strconv.ParseFloat(lngStr, 64)
		if err != nil {
			writeJSON(http.StatusBadRequest, map[string]string{"error": "invalid 'lng': " + err.Error()})
			return
		}

		detail, err := loc.GetLocationDetail(lat, lng)
		if err != nil {
			writeJSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(http.StatusOK, detail)
	})

	mux.HandleFunc("/v1/subdistricts-intersecting", func(w http.ResponseWriter, r *http.Request) {
		writeJSON := func(status int, body any) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(body)
		}

		if r.Method != http.MethodPost {
			writeJSON(http.StatusMethodNotAllowed, map[string]string{"error": "use POST with a GeoJSON geometry body"})
			return
		}

		var geometry thailocate.Geometry
		if err := json.NewDecoder(r.Body).Decode(&geometry); err != nil {
			writeJSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + err.Error()})
			return
		}

		matches, err := loc.FindSubdistrictsIntersecting(geometry)
		if err != nil {
			writeJSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(http.StatusOK, matches)
	})

	srv := &http.Server{
		Addr:         listenAddr,
		Handler:      logRequests(mux),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.String(), time.Since(start))
	})
}
