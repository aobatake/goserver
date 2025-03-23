package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPasswordHash(t *testing.T) {
	password := "superdupersecurepassword"
	hashed, err := HashPassword(password)
	if err != nil {
		t.Errorf("Hash is not hashed: %v", err)
	}
	err = CheckPasswordHash(password, hashed)
	if err != nil {
		t.Errorf("Password does not match: %v", err)
	}
}

func TestPasswordEmpty(t *testing.T) {
	password := ""
	_, err := HashPassword(password)
	if err == nil {
		t.Errorf("Password is supposed to be empty: %v", err)
	}
}

func TestJWT(t *testing.T) {
	userID := uuid.New()
	tokenSecret := "thisisthesecret"
	tokenStr, err := MakeJWT(userID, tokenSecret, 10*time.Second)
	if err != nil {
		t.Errorf("Can't create token: %v", err)
	}

	obtainedUserID, err := ValidateJWT(tokenStr, tokenSecret)
	if err != nil {
		t.Errorf("Can't validate token: %v", err)
	}
	if userID != obtainedUserID {
		t.Errorf("UserIDs don't match")
	}
}

func TestGetBearerToken(t *testing.T) {
	header := http.Header{}
	header.Add("Authorization", "Bearer TOKEN_STRING")
	token, err := GetBearerToken(header)
	if err != nil || token != "TOKEN_STRING" {
		t.Errorf("Can't parse Bearer Token")
	}
}

func TestGetAPIKey(t *testing.T) {
	header := http.Header{}
	header.Add("Authorization", "ApiKey KEYHERE")
	key, err := GetAPIKey(header)
	if err != nil || key != "KEYHERE" {
		t.Errorf("Can't parse API Key")
	}
}
