package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestAdditionalParsers(t *testing.T) {
	ns, e := parseNumbers([]byte(`<a class="card" href="/us-phone-number/123/"><strong>+1 202-555-0123</strong></a>`), "ReceiveSMS.co")
	if e != nil || len(ns) != 1 || ns[0].Phone != "+12025550123" || ns[0].Country != "United States" {
		t.Fatalf("%v %v", ns, e)
	}
	ms, e := parseAdditionalMessages([]byte(`<section id="sms-comments"><article class="entry-card"><div class="entry-left"><a class="from-link">Example</a></div><div class="entry-right"><span class="muted">1 minute ago</span></div><div class="sms">Example code 123456</div></article></section>`), "ReceiveSMS.co")
	if e != nil || len(ms) != 1 || ms[0].Code != "123456" {
		t.Fatalf("%v %v", ms, e)
	}
	ms, e = parseAdditionalMessages([]byte(`[{"mesaje_id":"123","telefon":"Example","mesaj":"Example code 123456","data":"2026-01-01 12:00:00"}]`), "SMS-Receive.net")
	if e != nil || len(ms) != 1 || ms[0].ID != "net-123" {
		t.Fatal(ms, e)
	}
	ms, e = parseAdditionalMessages([]byte(`{"response":1,"messages":{"data":[{"id":123,"text":"Example code 123456","in_number":"Example","created_at":"2026-01-01"}]}}`), "OnlineSim")
	if e != nil || len(ms) != 1 || ms[0].ID != "online-123" {
		t.Fatal(ms, e)
	}
	counts := historyCounts([]Message{{Sender: "OpenAI", Text: "code"}, {Text: "Telegram code"}, {Text: "generic code"}})
	if counts[0] != 1 || counts[1] != 1 || counts[2] != 0 {
		t.Fatal(counts)
	}
}
func TestAdditionalAllowlist(t *testing.T) {
	for _, u := range []string{"https://receivesms.co/us-phone-number/123/", "https://sms-receive.net/447700900123-UnitedKingdom", "https://onlinesim.io/free/352/621612137"} {
		if _, _, e := validateInbox(u); e != nil {
			t.Fatal(u, e)
		}
	}
	for _, u := range []string{"https://onlinesim.io/api/getBalance", "https://receivesms.co/admin/", "https://sms-receive.net/login.php", "https://onlinesim.io/free/352/621612137?x=1"} {
		if _, _, e := validateInbox(u); e == nil {
			t.Fatal("accepted", u)
		}
	}
}
func TestLiveSources(t *testing.T) {
	if os.Getenv("FREE_NUMBER_LIVE_TEST") != "1" {
		t.Skip("live tests are opt in")
	}
	a := &application{f: newFetcher()}
	tests := []struct{ source, list string }{{"Receive-SMSS", "https://receive-smss.com/"}, {"eSIM Plus", "https://esimplus.me/temporary-numbers"}, {"ReceiveSMS.co", "https://receivesms.co/active-numbers"}, {"SMS-Receive.net", "https://sms-receive.net/"}, {"OnlineSim", "https://onlinesim.io/api/getFreeList?lang=en&count=20"}}
	for _, tc := range tests {
		t.Run(strings.ReplaceAll(tc.source, "/", "-"), func(t *testing.T) {
			b, e := a.f.get(context.Background(), tc.list, 0)
			if e != nil {
				t.Fatal(e)
			}
			ns, e := parseNumbers(b, tc.source)
			if e != nil {
				t.Fatal(e)
			}
			if len(ns) == 0 {
				t.Fatal("no active numbers")
			}
			ms, e := a.loadInbox(context.Background(), ns[0].URL)
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("%s: %d numbers, sample inbox %d messages", tc.source, len(ns), len(ms))
		})
	}
}
