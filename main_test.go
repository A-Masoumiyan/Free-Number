package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReceiveNumbers(t *testing.T) {
	b := []byte(`<a href="/sms/447700900123/" aria-label="+447700900123 United Kingdom sms online">Open</a><a href="/sms/447700900123/">Open</a><a href="https://evil.test/sms/447700900124/">Bad</a><a href="/sms/12025550123/" aria-label="+12025550123 United States temporary phone number">Open</a>`)
	ns, e := parseNumbers(b, "Receive-SMSS")
	if e != nil || len(ns) != 2 {
		t.Fatalf("%v %v", ns, e)
	}
	if ns[0].Country != "United Kingdom" || ns[1].Country != "United States" {
		t.Fatalf("countries: %v", ns)
	}
}
func TestEsimNumbers(t *testing.T) {
	ns, e := parseNumbers([]byte(`<a href="/temporary-numbers/united-kingdom/447700900123">Open</a><a href="/temporary-numbers/canada/12025550123">Open</a><a href="/temporary-numbers/canada">Country</a>`), "eSIM Plus")
	if e != nil || len(ns) != 2 || ns[0].Country != "United Kingdom" {
		t.Fatalf("%v %v", ns, e)
	}
}
func TestReceiveMessagesStableIdentity(t *testing.T) {
	fixture := `<div class="row message_details"><div class="msgg"><label>Message</label><br><span>Your OpenAI code is <b>123456</b></span></div><div class="senderr"><label>Sender</label><a>OpenAI</a></div><div class="time"><label>Time</label>TIME</div></div><div class="row message_details"><script>advertisement</script></div>`
	a, e := parseReceiveMessages([]byte(strings.ReplaceAll(fixture, "TIME", "1 minute ago")))
	if e != nil || len(a) != 1 || a[0].Code != "123456" || a[0].Sender != "OpenAI" || strings.Contains(a[0].Text, "Message") {
		t.Fatalf("%+v %v", a, e)
	}
	b, _ := parseReceiveMessages([]byte(strings.ReplaceAll(fixture, "TIME", "2 minutes ago")))
	if a[0].ID != b[0].ID {
		t.Fatal("relative time changed message ID")
	}
}
func TestEsimMessages(t *testing.T) {
	b := []byte(`{"status":200,"data":[{"from":"12345","friendlyFrom":"Example","body":"Example code 123-456","receivedAt":"2026-01-01T12:00:00Z"},{"from":"12345","friendlyFrom":"Example","body":"Example code 123-456","receivedAt":"2026-01-01T12:01:00Z"}]}`)
	m, e := parseEsimMessages(b)
	if e != nil || len(m) != 2 || m[0].Code != "123456" || m[0].ID == m[1].ID {
		t.Fatalf("%+v %v", m, e)
	}
	m, e = parseEsimMessages([]byte(`{"status":200,"data":[]}`))
	if e != nil || len(m) != 0 {
		t.Fatal(e)
	}
}
func TestChangedOrBlockedResponse(t *testing.T) {
	if _, e := parseReceiveMessages([]byte(`<h1>Just a moment</h1>`)); e == nil {
		t.Fatal("challenge accepted")
	}
	if _, e := parseEsimMessages([]byte(`{"status":403,"data":[]}`)); e == nil {
		t.Fatal("error accepted")
	}
	if _, e := parseNumbers([]byte(`<h1>Blocked</h1>`), "Receive-SMSS"); e == nil {
		t.Fatal("empty source accepted")
	}
}
func TestInboxAllowlist(t *testing.T) {
	bad := []string{"http://receive-smss.com/sms/447700900123/", "https://receive-smss.com.evil.test/sms/447700900123/", "https://user@receive-smss.com/sms/447700900123/", "https://receive-smss.com:443/sms/447700900123/", "https://127.0.0.1/sms/447700900123/", "https://receive-smss.com/sms/447700900123/?x=1", "https://esimplus.me/temporary-numbers/uk/447700900123#x", "https://esimplus.me/temporary-numbers/../123456789"}
	for _, u := range bad {
		if _, _, e := validateInbox(u); e == nil {
			t.Errorf("accepted %s", u)
		}
	}
	got, source, e := validateInbox("https://esimplus.me/temporary-numbers/united-kingdom/447700900123")
	if e != nil || source != "eSIM Plus" || got != "https://esimplus.me/api/sms-receiver/447700900123/sms?perPage=50" {
		t.Fatal(got, source, e)
	}
}
func TestCachingCoalescesRequests(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write([]byte("ok")) }))
	defer s.Close()
	f := newFetcher()
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, e := f.get(context.Background(), s.URL, time.Minute)
			if e != nil || string(b) != "ok" {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("%d requests", calls.Load())
	}
}
func TestLoopbackAuthentication(t *testing.T) {
	a := &application{token: "secret", host: "127.0.0.1:12345"}
	for _, tc := range []struct {
		host, origin, token string
		status              int
	}{{a.host, "", "", 403}, {"evil.test", "", "secret", 403}, {a.host, "http://evil.test", "secret", 403}, {a.host, "http://" + a.host, "secret", 200}} {
		r := httptest.NewRequest("POST", "http://"+tc.host+"/api/ping", nil)
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-FreeNumber-Token", tc.token)
		w := httptest.NewRecorder()
		a.serve(w, r)
		if w.Code != tc.status {
			t.Errorf("%+v: %d", tc, w.Code)
		}
		if w.Code == 200 {
			var d map[string]bool
			if json.Unmarshal(w.Body.Bytes(), &d) != nil || !d["ok"] {
				t.Fatal("bad ping")
			}
		}
	}
}
