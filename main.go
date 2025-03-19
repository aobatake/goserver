package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
  "log"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func main() {
	mux := http.NewServeMux()
	apiCfg := apiConfig{
		fileserverHits: atomic.Int32{},
	}
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))
	mux.HandleFunc("GET /admin/metrics", apiCfg.metricsHandler)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetHandler)
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(http.StatusText(http.StatusOK)))
	})
	mux.HandleFunc("POST /api/validate_chirp", func(w http.ResponseWriter, r *http.Request) {
		type chirp struct {
			Body string `json:"body"`
		}
		decoder := json.NewDecoder(r.Body)
		ch := chirp{}
		err := decoder.Decode(&ch)
		if err != nil {
      respondError(w, "Something went wrong", 500, err)
      return
		}
		if len(ch.Body) > 140 {
      respondError(w, "Chirp is too long", 400, nil)
      return
		}
    validJSON := struct {
      Valid bool `json:"valid"`
    } {
      Valid: true,
    }
    respondJSON(w, 200, validJSON)

	})

	s := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	s.ListenAndServe()
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) resetHandler(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits.Store(0)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Reset"))

}

func (cfg *apiConfig) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("<html><body><h1>Welcome, Chirpy Admin</h1><p>Chirpy has been visited %d times!</p></body></html>", cfg.fileserverHits.Load())))
}

func respondError(w http.ResponseWriter, msg string, code int, err error)  {
  if err != nil {
    log.Println(err)
  }
  if code >= 500 {
    log.Printf("Responding with 5xx error: %v", err)
  }
  type errorResponse struct {
    Error string `json:"error"`
  }
  respondJSON(w, code, errorResponse{
    Error: msg,
  })

}

func respondJSON(w http.ResponseWriter, code int, payload any) {
  w.Header().Set("Content-Type", "application/json")
  data, err := json.Marshal(payload)
  if err != nil {
    log.Printf("Error Marshalling JSON: %v", err)
    w.WriteHeader(500)
  }
  w.WriteHeader(code)
  w.Write(data)
}
