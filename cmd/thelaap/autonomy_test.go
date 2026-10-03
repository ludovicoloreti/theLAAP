package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAutonomyRejectsCommandsAndCrossSiteWrites(t *testing.T) {
	for _, action := range []string{"start:foreign", "enable; touch /tmp/unsafe", "$(whoami)", "repair-model"} {
		if _, ok := autonomyArgs(action); ok {
			t.Fatalf("arbitrary action accepted: %q", action)
		}
	}
	sessionToken, listeningPort = "autonomy-test", 7070
	r := httptest.NewRequest(http.MethodPost, "/api/autonomia", strings.NewReader(`{"action":"enable"}`))
	r.RemoteAddr, r.Host = "127.0.0.1:54321", "127.0.0.1:7070"
	r.Header.Set("Origin", "https://foreign.invalid")
	r.Header.Set("X-theLAAP-Token", sessionToken)
	w := httptest.NewRecorder()
	rotte("").ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-site write returned %d", w.Code)
	}
}
