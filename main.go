package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/html"
)

//go:embed ui/*
var assets embed.FS

type Number struct {
	Phone   string `json:"phone"`
	Country string `json:"country"`
	Source  string `json:"source"`
	URL     string `json:"url"`
}
type Message struct {
	ID     string `json:"id"`
	Sender string `json:"sender"`
	Text   string `json:"text"`
	Time   string `json:"time"`
	Code   string `json:"code"`
}
type SourceStatus struct {
	Source string `json:"source"`
	Count  int    `json:"count"`
	Error  string `json:"error,omitempty"`
}
type cacheItem struct {
	body []byte
	err  error
	at   time.Time
}
type fetcher struct {
	mu     sync.Mutex
	cache  map[string]cacheItem
	locks  map[string]*sync.Mutex
	client *http.Client
}

func newFetcher() *fetcher {
	return &fetcher{cache: map[string]cacheItem{}, locks: map[string]*sync.Mutex{}, client: &http.Client{Timeout: 22 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || req.URL.Scheme != "https" || req.URL.Host != via[0].URL.Host {
			return errors.New("redirect rejected")
		}
		return nil
	}}}
}
func (f *fetcher) get(ctx context.Context, address string, ttl time.Duration) ([]byte, error) {
	f.mu.Lock()
	l := f.locks[address]
	if l == nil {
		l = &sync.Mutex{}
		f.locks[address] = l
	}
	f.mu.Unlock()
	l.Lock()
	defer l.Unlock()
	f.mu.Lock()
	c, ok := f.cache[address]
	f.mu.Unlock()
	if ok && time.Since(c.at) < ttl {
		return c.body, c.err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FreeNumber/0.1 (public SMS viewer)")
	req.Header.Set("Accept", "text/html,application/json")
	req.Header.Set("Cache-Control", "no-cache")
	if strings.HasPrefix(address, "https://sms-receive.net/get_sms_register.php") {
		req.Header.Set("X-Alt-Data", strconv.FormatInt(time.Now().Unix(), 10))
		req.Header.Set("Referer", "https://sms-receive.net/")
	}
	res, err := f.client.Do(req)
	var b []byte
	if err == nil {
		defer res.Body.Close()
		if res.StatusCode != 200 {
			err = fmt.Errorf("HTTP %d — دسترسی منبع محدود است؛ صفحهٔ اصلی را باز کنید", res.StatusCode)
		} else {
			b, err = io.ReadAll(io.LimitReader(res.Body, 8*1024*1024+1))
			if len(b) > 8*1024*1024 {
				err = errors.New("پاسخ منبع بیش از حد بزرگ است")
			}
		}
	}
	if err != nil {
		b = nil
		err = fmt.Errorf("دریافت ناموفق: %v", err)
	}
	f.mu.Lock()
	f.cache[address] = cacheItem{b, err, time.Now()}
	if len(f.cache) > 400 {
		for k, v := range f.cache {
			if time.Since(v.at) > 5*time.Minute {
				delete(f.cache, k)
				delete(f.locks, k)
			}
		}
	}
	f.mu.Unlock()
	return b, err
}
func attr(n *html.Node, k string) string {
	for _, a := range n.Attr {
		if a.Key == k {
			return a.Val
		}
	}
	return ""
}
func hasClass(n *html.Node, c string) bool {
	for _, v := range strings.Fields(attr(n, "class")) {
		if v == c {
			return true
		}
	}
	return false
}
func walk(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}
func nodeText(n *html.Node) string {
	var b strings.Builder
	var run func(*html.Node)
	run = func(x *html.Node) {
		if x.Type == html.ElementNode && (x.Data == "script" || x.Data == "style" || x.Data == "label") {
			return
		}
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
			b.WriteByte(' ')
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			run(c)
		}
	}
	run(n)
	return strings.Join(strings.Fields(b.String()), " ")
}
func classText(n *html.Node, c string) string {
	value := ""
	walk(n, func(x *html.Node) {
		if hasClass(x, c) {
			value = nodeText(x)
		}
	})
	return value
}

var receivePath = regexp.MustCompile(`^/sms/([0-9]{7,15})/$`)
var esimPath = regexp.MustCompile(`^/temporary-numbers/([a-z-]+)/([0-9]{7,15})/?$`)
var leadingCountry = regexp.MustCompile(`^\+[0-9]+\s+(.+?)(?:\s+(?:sms|receive|temporary|temp)\b.*)?$`)
var codePattern = regexp.MustCompile(`\b[0-9]{3}[- ][0-9]{3}\b|\b[0-9]{4,8}\b`)

func countryTitle(s string) string {
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}
func parseNumbers(b []byte, source string) ([]Number, error) {
	if source != "Receive-SMSS" && source != "eSIM Plus" {
		return parseAdditionalNumbers(b, source)
	}
	doc, e := html.Parse(strings.NewReader(string(b)))
	if e != nil {
		return nil, e
	}
	out := []Number{}
	seen := map[string]bool{}
	walk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "a" {
			return
		}
		h := attr(n, "href")
		var phone, country, address string
		if source == "Receive-SMSS" {
			m := receivePath.FindStringSubmatch(h)
			if m == nil {
				return
			}
			phone = "+" + m[1]
			if c := leadingCountry.FindStringSubmatch(attr(n, "aria-label")); len(c) > 1 {
				country = c[1]
			}
			address = "https://receive-smss.com" + h
		} else {
			m := esimPath.FindStringSubmatch(h)
			if m == nil {
				return
			}
			phone = "+" + m[2]
			country = countryTitle(m[1])
			address = "https://esimplus.me" + h
		}
		if !seen[phone] {
			seen[phone] = true
			if country == "" {
				country = "Unknown"
			}
			out = append(out, Number{phone, country, source, address})
		}
	})
	if len(out) == 0 {
		return out, errors.New("شماره‌ای استخراج نشد؛ احتمال تغییر صفحه یا نیاز به مرورگر وجود دارد")
	}
	return out, nil
}
func newMessage(sender, text, stamp string) Message {
	sum := sha256.Sum256([]byte(sender + "\x00" + text))
	return Message{hex.EncodeToString(sum[:12]), sender, text, stamp, strings.ReplaceAll(strings.ReplaceAll(codePattern.FindString(text), "-", ""), " ", "")}
}
func parseReceiveMessages(b []byte) ([]Message, error) {
	doc, e := html.Parse(strings.NewReader(string(b)))
	if e != nil {
		return nil, e
	}
	out := []Message{}
	seen := map[string]bool{}
	rows := 0
	walk(doc, func(n *html.Node) {
		if !hasClass(n, "message_details") {
			return
		}
		rows++
		text := classText(n, "msgg")
		if text == "" {
			return
		}
		m := newMessage(classText(n, "senderr"), text, classText(n, "time"))
		if !seen[m.ID] {
			seen[m.ID] = true
			out = append(out, m)
		}
	})
	if rows == 0 {
		return out, errors.New("قالب صندوق پیامک شناسایی نشد؛ صفحهٔ منبع را باز کنید")
	}
	return out, nil
}
func parseEsimMessages(b []byte) ([]Message, error) {
	var data struct {
		Data []struct {
			Body         string `json:"body"`
			From         string `json:"from"`
			FriendlyFrom string `json:"friendlyFrom"`
			ReceivedAt   string `json:"receivedAt"`
		} `json:"data"`
		Status int `json:"status"`
	}
	if e := json.Unmarshal(b, &data); e != nil {
		return nil, errors.New("پاسخ پیامک JSON معتبر نیست؛ صفحهٔ منبع را باز کنید")
	}
	if data.Status != 200 || data.Data == nil {
		return nil, fmt.Errorf("پاسخ نامعتبر سرویس پیامک (%d)", data.Status)
	}
	out := []Message{}
	seen := map[string]bool{}
	for _, x := range data.Data {
		sender := x.FriendlyFrom
		if sender == "" {
			sender = x.From
		}
		m := newMessage(sender, x.Body, x.ReceivedAt)
		sum := sha256.Sum256([]byte(m.ID + "\x00" + x.ReceivedAt))
		m.ID = hex.EncodeToString(sum[:12])
		if !seen[m.ID] {
			seen[m.ID] = true
			out = append(out, m)
		}
	}
	return out, nil
}
func validateInbox(raw string) (string, string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", errors.New("آدرس صندوق معتبر نیست")
	}
	if u.Host == "receive-smss.com" && receivePath.MatchString(u.Path) {
		return raw, "Receive-SMSS", nil
	}
	if u.Host == "esimplus.me" {
		if m := esimPath.FindStringSubmatch(u.Path); m != nil {
			return "https://esimplus.me/api/sms-receiver/" + m[2] + "/sms?perPage=50", "eSIM Plus", nil
		}
	}
	if u.Host == "receivesms.co" && receivesmsPath.MatchString(u.Path) {
		return raw, "ReceiveSMS.co", nil
	}
	if u.Host == "sms-receive.net" {
		if m := smsReceivePath.FindStringSubmatch(u.Path); m != nil {
			return "https://sms-receive.net/get_sms_register.php?phone=" + m[1], "SMS-Receive.net", nil
		}
	}
	if u.Host == "onlinesim.io" {
		if m := onlineSimPath.FindStringSubmatch(u.Path); m != nil {
			return "https://onlinesim.io/api/getFreeList?lang=en&count=50&country=" + m[1] + "&number=" + m[2], "OnlineSim", nil
		}
	}
	return "", "", errors.New("این آدرس در منابع پشتیبانی‌شده نیست")
}

type application struct {
	f           *fetcher
	token, host string
	lastSeen    atomic.Int64
	server      *http.Server
}

func (a *application) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func (a *application) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; object-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	if r.Host != a.host {
		http.Error(w, "Invalid host", 403)
		return
	}
	if o := r.Header.Get("Origin"); o != "" && o != "http://"+a.host {
		http.Error(w, "Invalid origin", 403)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method != "GET" {
			http.Error(w, "Method", 405)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		isFlag := regexp.MustCompile(`^flags/[a-z]{2}\.svg$`).MatchString(name)
		if name != "index.html" && name != "app.js" && name != "style.css" && name != "native-api.js" && !isFlag {
			http.NotFound(w, r)
			return
		}
		b, e := assets.ReadFile("ui/" + name)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		ct := map[string]string{"index.html": "text/html; charset=utf-8", "app.js": "text/javascript; charset=utf-8", "style.css": "text/css; charset=utf-8", "native-api.js": "text/javascript; charset=utf-8"}
		if isFlag {
			w.Header().Set("Content-Type", "image/svg+xml")
		} else {
			w.Header().Set("Content-Type", ct[name])
		}
		w.Write(b)
		return
	}
	if r.Method != "POST" || r.Header.Get("X-FreeNumber-Token") != a.token {
		http.Error(w, "Unauthorized", 403)
		return
	}
	a.lastSeen.Store(time.Now().Unix())
	switch r.URL.Path {
	case "/api/ping":
		a.json(w, map[string]bool{"ok": true})
	case "/api/quit":
		a.json(w, map[string]bool{"ok": true})
		go func() { time.Sleep(300 * time.Millisecond); a.server.Shutdown(context.Background()) }()
	case "/api/numbers":
		jobs := []struct{ source, url string }{{"Receive-SMSS", "https://receive-smss.com/"}, {"eSIM Plus", "https://esimplus.me/temporary-numbers"}, {"eSIM Plus · UK", "https://esimplus.me/temporary-numbers/united-kingdom"}, {"ReceiveSMS.co", "https://receivesms.co/active-numbers"}, {"SMS-Receive.net", "https://sms-receive.net/"}, {"OnlineSim", "https://onlinesim.io/api/getFreeList?lang=en&count=20"}}
		var filter struct {
			Country string `json:"country"`
		}
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&filter)
		if filter.Country != "" {
			jobs = nil
			slug := strings.ToLower(strings.ReplaceAll(filter.Country, " ", "-"))
			valid := false
			for _, n := range countryCodes {
				if n == filter.Country {
					valid = true
				}
			}
			if !valid {
				http.Error(w, "Unsupported country", 400)
				return
			}
			jobs = append(jobs, struct{ source, url string }{"eSIM Plus", "https://esimplus.me/temporary-numbers/" + slug})
			prefixes := map[string]string{"United Kingdom": "british-phone-numbers/gb", "United States": "us-phone-numbers/us", "Canada": "canadian-phone-numbers/ca", "Germany": "german-phone-numbers/de", "France": "french-phone-numbers/fr", "Netherlands": "dutch-phone-numbers/nl", "India": "indian-phone-numbers/in"}
			if p := prefixes[filter.Country]; p != "" {
				jobs = append(jobs, struct{ source, url string }{"ReceiveSMS.co", "https://receivesms.co/" + p + "/"})
			}
			if c := onlineSimCountries[filter.Country]; c != "" {
				jobs = append(jobs, struct{ source, url string }{"OnlineSim", "https://onlinesim.io/api/getFreeList?lang=en&count=20&country=" + c})
			}
		}
		type result struct {
			numbers []Number
			status  SourceStatus
		}
		ch := make(chan result, len(jobs))
		sem := make(chan struct{}, 4)
		for _, job := range jobs {
			go func(source, address string) {
				sem <- struct{}{}
				defer func() { <-sem }()
				base := source
				if strings.HasPrefix(base, "eSIM") {
					base = "eSIM Plus"
				}
				b, e := a.f.get(r.Context(), address, 2*time.Minute)
				nums := []Number{}
				if e == nil {
					nums, e = parseNumbers(b, base)
				}
				st := SourceStatus{Source: source, Count: len(nums)}
				if e != nil {
					st.Error = e.Error()
				}
				ch <- result{nums, st}
			}(job.source, job.url)
		}
		nums := []Number{}
		sts := []SourceStatus{}
		seen := map[string]bool{}
		for range jobs {
			x := <-ch
			sts = append(sts, x.status)
			for _, n := range x.numbers {
				k := n.Source + "|" + n.Phone
				if !seen[k] {
					seen[k] = true
					nums = append(nums, n)
				}
			}
		}
		sort.Slice(nums, func(i, j int) bool {
			if nums[i].Country != nums[j].Country {
				return nums[i].Country < nums[j].Country
			}
			return nums[i].Phone < nums[j].Phone
		})
		sort.Slice(sts, func(i, j int) bool { return sts[i].Source < sts[j].Source })
		a.json(w, map[string]any{"numbers": nums, "sources": sts, "fetchedAt": time.Now().Format(time.RFC3339)})
	case "/api/history":
		var input struct {
			URL string `json:"url"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input) != nil {
			http.Error(w, "Bad JSON", 400)
			return
		}
		ms, e := a.loadInbox(r.Context(), input.URL)
		if e != nil {
			a.json(w, map[string]any{"error": e.Error()})
			return
		}
		a.json(w, map[string]any{"counts": historyCounts(ms), "scanned": len(ms), "checkedAt": time.Now().Format(time.RFC3339)})
	case "/api/inbox":
		var input struct {
			URL string `json:"url"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input) != nil {
			http.Error(w, "Bad JSON", 400)
			return
		}
		ms, e := a.loadInbox(r.Context(), input.URL)
		if e != nil {
			a.json(w, map[string]any{"error": e.Error()})
			return
		}
		a.json(w, map[string]any{"messages": ms, "checkedAt": time.Now().Format(time.RFC3339)})
	default:
		http.NotFound(w, r)
	}
}
func openBrowser(address string) {
	if runtime.GOOS == "windows" {
		for _, root := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
			if root == "" {
				continue
			}
			p := filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe")
			if _, e := os.Stat(p); e == nil {
				if exec.Command(p, "--app="+address, "--window-size=1260,860").Start() == nil {
					return
				}
			}
		}
		exec.Command("rundll32", "url.dll,FileProtocolHandler", address).Start()
	} else if runtime.GOOS == "darwin" {
		exec.Command("open", address).Start()
	} else {
		exec.Command("xdg-open", address).Start()
	}
}
func main() {
	noOpen := flag.Bool("no-open", false, "Do not open browser")
	port := flag.Int("port", 0, "Local listening port (0 = automatic)")
	flag.Parse()
	l, e := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", *port))
	if e != nil {
		log.Fatal(e)
	}
	b := make([]byte, 32)
	if _, e = rand.Read(b); e != nil {
		log.Fatal(e)
	}
	a := &application{f: newFetcher(), token: hex.EncodeToString(b), host: l.Addr().String()}
	a.lastSeen.Store(time.Now().Unix())
	a.server = &http.Server{Handler: http.HandlerFunc(a.serve), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 55 * time.Second, IdleTimeout: 60 * time.Second}
	address := "http://" + a.host + "/#" + a.token
	fmt.Println(address)
	if !*noOpen {
		openBrowser(address)
	}
	go func() {
		for range time.NewTicker(30 * time.Second).C {
			if time.Now().Unix()-a.lastSeen.Load() > 180 {
				a.server.Shutdown(context.Background())
				return
			}
		}
	}()
	if e = a.server.Serve(l); e != http.ErrServerClosed {
		log.Print(e)
	}
}
