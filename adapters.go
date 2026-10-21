package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/net/html"
	"regexp"
	"strings"
	"time"
)

var receivesmsPath = regexp.MustCompile(`^/([a-z]{2})-phone-number/([0-9]{1,8})/$`)
var smsReceivePath = regexp.MustCompile(`^/([0-9]{7,15})-([A-Za-z]+)$`)
var onlineSimPath = regexp.MustCompile(`^/free/([0-9]{1,3})/([0-9]{3,15})$`)
var phoneText = regexp.MustCompile(`\+[0-9][0-9 ()-]{6,22}[0-9]`)
var countryCodes = map[string]string{"us": "United States", "gb": "United Kingdom", "de": "Germany", "fr": "France", "ca": "Canada", "my": "Malaysia", "fi": "Finland", "bg": "Bulgaria", "co": "Colombia", "in": "India", "mx": "Mexico", "tw": "Taiwan", "th": "Thailand", "pr": "Puerto Rico", "vn": "Vietnam", "se": "Sweden", "es": "Spain", "nl": "Netherlands", "br": "Brazil", "ng": "Nigeria", "il": "Israel", "ph": "Philippines", "cn": "China", "sg": "Singapore", "au": "Australia", "lu": "Luxembourg", "lk": "Sri Lanka", "no": "Norway", "nz": "New Zealand", "be": "Belgium", "dk": "Denmark", "sk": "Slovakia", "hu": "Hungary", "cy": "Cyprus", "pl": "Poland", "hr": "Croatia", "ch": "Switzerland", "ie": "Ireland", "ar": "Argentina", "ee": "Estonia", "at": "Austria"}

func parseAdditionalNumbers(b []byte, source string) ([]Number, error) {
	if source == "OnlineSim" {
		var d struct {
			Response json.RawMessage `json:"response"`
			Numbers  map[string]struct {
				Country int    `json:"country"`
				Slug    string `json:"country_original"`
				Phone   string `json:"full_number"`
				Archive bool   `json:"is_archive"`
			} `json:"numbers"`
		}
		if json.Unmarshal(b, &d) != nil || string(d.Response) != "1" {
			return nil, errors.New("پاسخ معتبر OnlineSim دریافت نشد")
		}
		out := []Number{}
		for national, n := range d.Numbers {
			if n.Archive {
				continue
			}
			out = append(out, Number{"+" + strings.TrimPrefix(n.Phone, "+"), countryTitle(n.Slug), source, fmt.Sprintf("https://onlinesim.io/free/%d/%s", n.Country, national)})
		}
		if len(out) == 0 {
			return out, errors.New("شمارهٔ فعال موجود نیست")
		}
		return out, nil
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
		if source == "ReceiveSMS.co" {
			m := receivesmsPath.FindStringSubmatch(h)
			if m == nil {
				return
			}
			p := phoneText.FindString(nodeText(n))
			p = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(p)
			if p == "" {
				return
			}
			phone = p
			country = countryCodes[m[1]]
			address = "https://receivesms.co" + h
		} else if source == "SMS-Receive.net" {
			h = "/" + strings.TrimPrefix(h, "/")
			m := smsReceivePath.FindStringSubmatch(h)
			if m == nil {
				return
			}
			phone = "+" + m[1]
			country = m[2]
			if country == "UnitedKingdom" {
				country = "United Kingdom"
			}
			if country == "UnitedStates" || country == "USA" {
				country = "United States"
			}
			address = "https://sms-receive.net" + h
		} else {
			return
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
		return out, errors.New("فهرست فعال قابل استخراج نیست")
	}
	return out, nil
}
func parseAdditionalMessages(b []byte, source string) ([]Message, error) {
	out := []Message{}
	if source == "ReceiveSMS.co" {
		doc, e := html.Parse(strings.NewReader(string(b)))
		if e != nil {
			return nil, e
		}
		found := false
		walk(doc, func(n *html.Node) {
			if attr(n, "id") == "sms-comments" {
				found = true
			}
			if !hasClass(n, "entry-card") {
				return
			}
			text := classText(n, "sms")
			if text == "" {
				return
			}
			sender := classText(n, "from-link")
			if sender == "" {
				sender = strings.TrimSpace(strings.TrimPrefix(classText(n, "entry-left"), "From"))
			}
			stamp := ""
			walk(n, func(x *html.Node) {
				if hasClass(x, "muted") {
					stamp = nodeText(x)
				}
			})
			out = append(out, newMessage(sender, text, stamp))
		})
		if !found {
			return nil, errors.New("قالب صندوق ReceiveSMS.co شناسایی نشد")
		}
		return out, nil
	}
	if source == "SMS-Receive.net" {
		var rows []struct {
			ID     json.RawMessage `json:"mesaje_id"`
			Sender string          `json:"telefon"`
			Text   string          `json:"mesaj"`
			Time   string          `json:"data"`
		}
		if json.Unmarshal(b, &rows) != nil || rows == nil {
			return nil, errors.New("پاسخ پیامک SMS-Receive.net معتبر نیست")
		}
		for _, x := range rows {
			m := newMessage(x.Sender, x.Text, x.Time)
			m.ID = "net-" + strings.Trim(string(x.ID), "\"")
			out = append(out, m)
		}
		return out, nil
	}
	if source == "OnlineSim" {
		var d struct {
			Response json.RawMessage `json:"response"`
			Messages struct {
				Data []struct {
					ID     json.RawMessage `json:"id"`
					Text   string          `json:"text"`
					Sender string          `json:"in_number"`
					Time   string          `json:"created_at"`
				} `json:"data"`
			} `json:"messages"`
		}
		if json.Unmarshal(b, &d) != nil || string(d.Response) != "1" || d.Messages.Data == nil {
			return nil, errors.New("صندوق OnlineSim قابل خواندن نیست")
		}
		for _, x := range d.Messages.Data {
			m := newMessage(x.Sender, x.Text, x.Time)
			if len(x.ID) > 0 {
				m.ID = "online-" + strings.Trim(string(x.ID), "\"")
			} else {
				sum := sha256.Sum256([]byte(m.ID + x.Time))
				m.ID = hex.EncodeToString(sum[:12])
			}
			out = append(out, m)
		}
		return out, nil
	}
	return nil, errors.New("منبع پشتیبانی نمی‌شود")
}
func (a *application) loadInbox(ctx context.Context, raw string) ([]Message, error) {
	address, source, e := validateInbox(raw)
	if e != nil {
		return nil, e
	}
	b, e := a.f.get(ctx, address, 30*time.Second)
	if e != nil {
		return nil, e
	}
	switch source {
	case "Receive-SMSS":
		return parseReceiveMessages(b)
	case "eSIM Plus":
		return parseEsimMessages(b)
	default:
		return parseAdditionalMessages(b, source)
	}
}

var platformWords = [][]string{{"openai", "open ai", "chatgpt"}, {"telegram", "تلگرام"}, {"whatsapp", "واتساب", "واتساپ"}, {"google", "gmail", "گوگل"}, {"apple", "icloud", "itunes", "اپل"}, {"instagram", "اینستاگرام"}, {"facebook", "فیسبوک"}, {"microsoft", "outlook", "مایکروسافت"}}

func historyCounts(ms []Message) []int {
	counts := make([]int, len(platformWords))
	for _, m := range ms {
		t := strings.ToLower(m.Sender + " " + m.Text)
		for i, words := range platformWords {
			for _, w := range words {
				if strings.Contains(t, w) {
					counts[i]++
					break
				}
			}
		}
	}
	return counts
}

var onlineSimCountries = map[string]string{"Luxembourg": "352", "Ireland": "353", "Cyprus": "357", "Finland": "358", "Bulgaria": "359", "Estonia": "372", "Croatia": "385", "Slovakia": "421", "New Zealand": "64", "Australia": "61", "Colombia": "57", "Argentina": "54", "Poland": "48", "Norway": "47", "Denmark": "45", "Austria": "43", "Switzerland": "41", "Hungary": "36", "France": "33", "Belgium": "32"}
