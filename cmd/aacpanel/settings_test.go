package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettingsAnswerWithoutDB(t *testing.T) {
	srv := &Server{hostName: "STAND-01"}

	w := httptest.NewRecorder()
	srv.apiSettings(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("settings without a database gave %d, expected 200", w.Code)
	}

	var body struct {
		Settings []struct {
			Key    string `json:"key"`
			Value  string `json:"value"`
			Secret bool   `json:"secret"`
			Cost   string `json:"cost"`
		} `json:"settings"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Settings) == 0 {
		t.Fatal("the list of settings is empty")
	}
	for _, it := range body.Settings {
		if it.Secret && it.Value != "" {
			t.Errorf("%s: the value of a secret went out to the page", it.Key)
		}
		if it.Cost == "" {
			t.Errorf("%s: the price of the change is not named", it.Key)
		}
	}
}

func TestSettingsKeepSecretsOutOfTheAnswer(t *testing.T) {
	const secret = "secret-that-must-not-be-in-the-answer"
	t.Setenv("AACP_SECRET", secret)

	srv := &Server{hostName: "STAND-01"}
	w := httptest.NewRecorder()
	srv.apiSettings(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))

	if strings.Contains(w.Body.String(), secret) {
		t.Error("the value of the secret is visible in the answer of the settings handler")
	}
}
