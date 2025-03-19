package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"database/sql"
	"github.com/aobatake/internal/database"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return
	}
	dbQueries := database.New(db)
	mux := http.NewServeMux()
	apiCfg := apiConfig{
		fileserverHits: atomic.Int32{},
		db:             dbQueries,
	}
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))
	mux.HandleFunc("GET /admin/metrics", apiCfg.metricsHandler)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetHandler)
	mux.HandleFunc("GET /api/healthz", healthzHandler)
	mux.HandleFunc("POST /api/validate_chirp", validateChirpHandler)

	s := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	s.ListenAndServe()
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(http.StatusText(http.StatusOK)))
}

func validateChirpHandler(w http.ResponseWriter, r *http.Request) {
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

	msg := censorMsg(ch.Body, "kerfuffle")
	msg = censorMsg(msg, "sharbert")
	msg = censorMsg(msg, "fornax")

	responseJSON := struct {
		Cleaned_body string `json:"cleaned_body"`
	}{
		Cleaned_body: msg,
	}
	respondJSON(w, 200, responseJSON)
}

func censorMsg(msg string, censorWord string) string {
	split_msg := strings.Split(msg, " ")
	s := make([]string, len(split_msg))
	for i, word := range split_msg {
		if strings.ToLower(word) == censorWord {
			s[i] = "****"
		} else {
			s[i] = word
		}
	}
	return strings.Join(s, " ")
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
