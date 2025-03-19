package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func respondError(w http.ResponseWriter, msg string, code int, err error) {
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
