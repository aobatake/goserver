package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"database/sql"

	"github.com/aobatake/goserver/internal/auth"
	"github.com/aobatake/goserver/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type APIConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
	JWTSecret      string
	polkaSecret    string
}

type User struct {
	ID           uuid.UUID `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Email        string    `json:"email"`
	Token        string    `json:"token"`
	RefreshToken string    `json:"refresh_token"`
	IsChirpyRed  bool      `json:"is_chirpy_red"`
}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	User_id   uuid.UUID `json:"user_id"`
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	platform := os.Getenv("PLATFORM")
	JWTSecret := os.Getenv("JWT_SECRET")
	polkaSecret := os.Getenv("POLKA_KEY")

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return
	}
	dbQueries := database.New(db)

	mux := http.NewServeMux()
	ap := APIConfig{
		fileserverHits: atomic.Int32{},
		db:             dbQueries,
		platform:       platform,
		JWTSecret:      JWTSecret,
		polkaSecret:    polkaSecret,
	}
	mux.Handle("/app/", ap.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))
	mux.HandleFunc("GET /admin/metrics", ap.metricsHandler)
	mux.HandleFunc("POST /admin/reset", ap.resetHandler)

	mux.HandleFunc("GET /api/healthz", healthzHandler)

	mux.HandleFunc("POST /api/chirps", ap.chirpHandler)
	mux.HandleFunc("GET /api/chirps", ap.getChirpsHandler)
	mux.HandleFunc("GET /api/chirps/{chirpID}", ap.getChirpHandler)
	mux.HandleFunc("DELETE /api/chirps/{chirpID}", ap.DeleteChirpHandler)

	mux.HandleFunc("POST /api/users", ap.createUsersHandler)
	mux.HandleFunc("PUT /api/users", ap.updateUsersHandler)

	mux.HandleFunc("POST /api/login", ap.loginHandler)
	mux.HandleFunc("POST /api/refresh", ap.refreshTokenHandler)
	mux.HandleFunc("POST /api/revoke", ap.revokeRefreshTokenHandler)

	mux.HandleFunc("POST /api/polka/webhooks", ap.PolkaHandler)

	s := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	s.ListenAndServe()
}

func (c *APIConfig) loginHandler(w http.ResponseWriter, r *http.Request) {
	type Login struct {
		Password         string `json:"password"`
		Email            string `json:"email"`
		ExpiresInSeconds int    `json:"expires_in_seconds"`
	}

	decoder := json.NewDecoder(r.Body)
	login := Login{}
	err := decoder.Decode(&login)
	if err != nil {
		respondError(w, "Error when parsing JSON msg", 500, err)
		return
	}

	user, err := c.db.GetUser(r.Context(), login.Email)
	if err != nil {
		respondError(w, "User with this email doesn't exist", 500, err)
		return
	}

	err = auth.CheckPasswordHash(login.Password, user.HashedPassword)
	if err != nil {
		respondError(w, "401 Unauthorized", 401, err)
		return
	}

	expiresInSeconds := 360
	if login.ExpiresInSeconds != 0 && login.ExpiresInSeconds < 360 {
		expiresInSeconds = login.ExpiresInSeconds
	}

	token, err := auth.MakeJWT(user.ID, c.JWTSecret, time.Duration(expiresInSeconds)*time.Second)
	if err != nil {
		respondError(w, "Can't create token", 500, err)
		return
	}

	rToken, err := auth.MakeRefreshToken()
	if err != nil {
		respondError(w, "Can't create refresh token", 500, err)
		return
	}

	err = c.db.StoreRefreshToken(r.Context(), database.StoreRefreshTokenParams{
		Token:     rToken,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(1440 * time.Hour),
	})
	if err != nil {
		respondError(w, "Error when storing refresh token", 500, err)
		return
	}

	userJSON := User{
		ID:           user.ID,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
		Email:        user.Email,
		Token:        token,
		RefreshToken: rToken,
		IsChirpyRed:  user.IsChirpyRed,
	}

	respondJSON(w, 200, userJSON)
}

func (c *APIConfig) revokeRefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondError(w, "Authorization Header doesn't have token", 500, err)
		return
	}

	err = c.db.RevokeToken(r.Context(), token)
	if err != nil {
		respondError(w, "Can't revoke token", 500, err)
		return
	}

	w.WriteHeader(204)
}

func (c *APIConfig) refreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondError(w, "Authorization Header doesn't have token", 500, err)
		return
	}
	tokenInfo, err := c.db.GetRefreshToken(r.Context(), token)
	if err != nil {
		respondError(w, "RefreshToken Invalid", 401, err)
		return
	}
	if !(time.Now().Before(tokenInfo.ExpiresAt)) || tokenInfo.RevokedAt.Valid == true {
		respondError(w, "RefreshToken Invalid", 401, err)
		return
	}

	token, err = auth.MakeJWT(tokenInfo.UserID, c.JWTSecret, time.Duration(360*time.Second))
	if err != nil {
		respondError(w, "Can't create token", 500, err)
		return
	}
	tokenJSON := struct {
		Token string `json:"token"`
	}{
		Token: token,
	}

	respondJSON(w, 200, tokenJSON)
}

func (c *APIConfig) updateUsersHandler(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondError(w, "Authorization Header doesn't have token", 401, err)
		return
	}

	userID, err := auth.ValidateJWT(token, c.JWTSecret)
	if err != nil {
		respondError(w, "Unauthorized or Token Invalid", 401, err)
		return
	}

	decoder := json.NewDecoder(r.Body)
	b := reqBody{}
	err = decoder.Decode(&b)
	if err != nil {
		respondError(w, "Something went wrong", 500, err)
		return
	}

	hashedPassword, err := auth.HashPassword(b.Password)

	user, err := c.db.UpdateUser(r.Context(), database.UpdateUserParams{
		ID:             userID,
		Email:          b.Email,
		HashedPassword: hashedPassword,
	})
	if err != nil {
		respondError(w, "Update User Error", 500, err)
		return
	}

	userJSON := User{
		ID:          user.ID,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
		Email:       user.Email,
		IsChirpyRed: user.IsChirpyRed,
	}

	respondJSON(w, 200, userJSON)
}

func (apiCfg *APIConfig) createUsersHandler(w http.ResponseWriter, r *http.Request) {
	type body struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}

	decoder := json.NewDecoder(r.Body)
	b := body{}
	err := decoder.Decode(&b)
	if err != nil {
		respondError(w, "Something went wrong", 500, err)
		return
	}

	hashedPassword, err := auth.HashPassword(b.Password)
	if err != nil {
		respondError(w, "Can't hash password", 500, err)
		return
	}

	user, err := apiCfg.db.CreateUser(r.Context(), database.CreateUserParams{
		Email:          b.Email,
		HashedPassword: hashedPassword,
	})
	if err != nil {
		respondError(w, "Can't create user", 500, err)
		return
	}

	userJSON := User{
		ID:          user.ID,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
		Email:       user.Email,
		IsChirpyRed: user.IsChirpyRed,
	}

	respondJSON(w, 201, userJSON)
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(http.StatusText(http.StatusOK)))
}
func (c *APIConfig) DeleteChirpHandler(w http.ResponseWriter, r *http.Request) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondError(w, "Authorization Header doesn't have token", 401, err)
		return
	}

	userID, err := auth.ValidateJWT(token, c.JWTSecret)
	if err != nil {
		respondError(w, "Unauthorized or Token Invalid", 401, err)
		return
	}

	chirpID, err := uuid.Parse(r.PathValue("chirpID"))
	if err != nil {
		respondError(w, "Can't parse chirpID", 500, err)
		return
	}

	ch, err := c.db.GetChirp(r.Context(), chirpID)
	if err != nil {
		respondError(w, "Can't get chirp", 404, err)
		return
	}

	if ch.UserID != userID {
		respondError(w, "User is not author of chirp", 403, err)
		return
	}

	err = c.db.DeleteChirp(r.Context(), chirpID)
	if err != nil {
		respondError(w, "Unable to delete chirp", 404, err)
		return
	}

	w.WriteHeader(204)
}

func (cfg *APIConfig) getChirpHandler(w http.ResponseWriter, r *http.Request) {
	chirpID, err := uuid.Parse(r.PathValue("chirpID"))
	if err != nil {
		respondError(w, "Can't parse UUID", 500, err)
		return
	}

	ch, err := cfg.db.GetChirp(r.Context(), chirpID)
	if err != nil {
		respondError(w, "Can't get chirp", 404, err)
		return
	}
	cc := Chirp{
		ID:        ch.ID,
		CreatedAt: ch.CreatedAt,
		UpdatedAt: ch.UpdatedAt,
		Body:      ch.Body,
		User_id:   ch.UserID,
	}

	respondJSON(w, 200, cc)
}

func (cfg *APIConfig) getChirpsHandler(w http.ResponseWriter, r *http.Request) {
	var authorID uuid.UUID
	var err error

	AIDParam := r.URL.Query().Get("author_id")
	filterByUserID := false
	if AIDParam != "" {
		authorID, err = uuid.Parse(AIDParam)
		if err != nil {
			respondError(w, "Can't parse authorID", 500, err)
			return

		}
		filterByUserID = true
	}

	chirps, err := cfg.db.GetChirps(r.Context(), database.GetChirpsParams{
		UserID:         authorID,
		FilterByUserID: filterByUserID,
	})
	if err != nil {
		respondError(w, "Can't get chirps", 500, err)
		return
	}

	s := r.URL.Query().Get("sort")
	if s == "desc" {
		sort.Slice(chirps, func(i, j int) bool {
			return chirps[i].CreatedAt.After(chirps[j].CreatedAt)
		})
	}
	cc := make([]Chirp, len(chirps))
	for i, ch := range chirps {
		cc[i] = Chirp{
			ID:        ch.ID,
			CreatedAt: ch.CreatedAt,
			UpdatedAt: ch.UpdatedAt,
			Body:      ch.Body,
			User_id:   ch.UserID,
		}
	}

	respondJSON(w, 200, cc)
}

func (c *APIConfig) chirpHandler(w http.ResponseWriter, r *http.Request) {
	type chirp struct {
		Body    string `json:"body"`
		User_id string `json:"user_id"`
	}
	decoder := json.NewDecoder(r.Body)
	ch := chirp{}
	err := decoder.Decode(&ch)
	if err != nil {
		respondError(w, "Something went wrong", 500, err)
		return
	}
	if len(ch.Body) > 140 {
		respondError(w, "Chirp is too long", 400, err)
		return
	}

	msg := censorMsg(ch.Body, "kerfuffle")
	msg = censorMsg(msg, "sharbert")
	msg = censorMsg(msg, "fornax")

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondError(w, "Invalid Authorization Header", 500, err)
		return
	}

	userID, err := auth.ValidateJWT(token, c.JWTSecret)
	if err != nil {
		respondError(w, "Token invalid", 401, err)
		return
	}

	cc, err := c.db.CreateChirp(r.Context(), database.CreateChirpParams{
		Body:   msg,
		UserID: userID,
	})
	if err != nil {
		return
	}

	chirpResponse := Chirp{
		ID:        cc.ID,
		CreatedAt: cc.CreatedAt,
		UpdatedAt: cc.UpdatedAt,
		Body:      cc.Body,
		User_id:   cc.UserID,
	}

	respondJSON(w, 201, chirpResponse)
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

func (cfg *APIConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *APIConfig) resetHandler(w http.ResponseWriter, r *http.Request) {
	if cfg.platform != "dev" {
		w.WriteHeader(403)
		w.Write([]byte("403 Forbidden"))
		return
	}
	cfg.fileserverHits.Store(0)
	err := cfg.db.DeleteAllUsers(r.Context())
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte("Can't delete all users"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Reset"))

}

func (c *APIConfig) PolkaHandler(w http.ResponseWriter, r *http.Request) {
	type Data struct {
		UserID string `json:"user_id"`
	}

	type JSONRequest struct {
		Event string `json:"event"`
		Data  `json:"data"`
	}

	key, err := auth.GetAPIKey(r.Header)
	if err != nil {
		respondError(w, "Authorization Header Invalid", 401, err)
		return
	}

	if key != c.polkaSecret {
		respondError(w, "API key invalid", 401, err)
		return
	}

	decoder := json.NewDecoder(r.Body)
	jr := JSONRequest{}
	err = decoder.Decode(&jr)
	if err != nil {
		respondError(w, "Can't decode JSON Request", 500, err)
		return
	}

	if jr.Event != "user.upgraded" {
		w.WriteHeader(204)
	}

	userID, err := uuid.Parse(jr.Data.UserID)
	if err != nil {
		respondError(w, "Can't parse chirpID", 500, err)
		return
	}

	err = c.db.UpgradeUser(r.Context(), userID)
	if err != nil {
		respondError(w, "Can't upgrade user to Chirpy Red", 404, err)
		return
	}

	w.WriteHeader(204)

}

func (cfg *APIConfig) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("<html><body><h1>Welcome, Chirpy Admin</h1><p>Chirpy has been visited %d times!</p></body></html>", cfg.fileserverHits.Load())))
}
