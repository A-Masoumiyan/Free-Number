package main

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTelegramReportsPersistAcrossApplicationPorts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "reports.json")
	a := &application{host: "127.0.0.1:10001", token: "secret", reports: reportStore{path: path}}
	request := func(app *application, route, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://"+app.host+route, strings.NewReader(body))
		r.Host = app.host
		r.Header.Set("X-FreeNumber-Token", app.token)
		w := httptest.NewRecorder()
		app.serve(w, r)
		return w
	}
	for _, state := range []string{"invalid", "banned", "code-received"} {
		w := request(a, "/api/telegram-report", `{"phone":"+447700900123","state":"`+state+`"}`)
		if w.Code != 200 || strings.Contains(w.Body.String(), `"error"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	b := &application{host: "127.0.0.1:10002", token: "next", reports: reportStore{path: path}}
	w := request(b, "/api/telegram-reports", `{}`)
	var data struct {
		Reports map[string]telegramReport `json:"reports"`
	}
	if json.Unmarshal(w.Body.Bytes(), &data) != nil || data.Reports["+447700900123"].State != "code-received" || data.Reports["+447700900123"].At <= 0 {
		t.Fatal(w.Body.String())
	}
	for _, body := range []string{`{"phone":"../../file","state":"banned"}`, `{"phone":"+447700900123","state":"verified"}`} {
		if w := request(b, "/api/telegram-report", body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request(b, "/api/telegram-report", `{"phone":"+447700900123","state":"clear"}`); strings.Contains(w.Body.String(), "447700900123") {
		t.Fatal(w.Body.String())
	}
}
func TestEmbeddedQualityScripts(t *testing.T) {
	a := &application{host: "127.0.0.1:10001"}
	for _, path := range []string{"/vendor/phone.js", "/number-quality.js"} {
		r := httptest.NewRequest("GET", "http://"+a.host+path, nil)
		r.Host = a.host
		w := httptest.NewRecorder()
		a.serve(w, r)
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") || w.Body.Len() == 0 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}
