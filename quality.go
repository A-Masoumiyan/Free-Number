package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

type telegramReport struct {
	State string `json:"state"`
	At    int64  `json:"at"`
}
type reportStore struct {
	mu   sync.Mutex
	path string
}

var reportPhone = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

func reportState(s string) bool { return s == "invalid" || s == "banned" || s == "code-received" }
func (s *reportStore) file() (string, error) {
	if s.path != "" {
		return s.path, nil
	}
	dir, e := os.UserConfigDir()
	if e != nil {
		return "", e
	}
	return filepath.Join(dir, "Free Number", "telegram-reports.json"), nil
}
func (s *reportStore) read() (map[string]telegramReport, error) {
	path, e := s.file()
	if e != nil {
		return nil, e
	}
	f, e := os.Open(path)
	if os.IsNotExist(e) {
		return map[string]telegramReport{}, nil
	}
	if e != nil {
		return nil, e
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, 2*1024*1024+1))
	if e != nil {
		return nil, e
	}
	if len(data) > 2*1024*1024 {
		return nil, errors.New("report store too large")
	}
	reports := map[string]telegramReport{}
	if e = json.Unmarshal(data, &reports); e != nil {
		return nil, e
	}
	if reports == nil {
		reports = map[string]telegramReport{}
	}
	for phone, r := range reports {
		if !reportPhone.MatchString(phone) || !reportState(r.State) || r.At <= 0 || r.At > time.Now().UnixMilli() {
			delete(reports, phone)
		}
	}
	return reports, nil
}
func (s *reportStore) handle(w http.ResponseWriter, r *http.Request, a *application, write bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reports, e := s.read()
	if e != nil {
		a.json(w, map[string]string{"error": "خواندن گزارش‌های ذخیره‌شده ممکن نشد"})
		return
	}
	if write {
		var input struct {
			Phone string `json:"phone"`
			State string `json:"state"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil || !reportPhone.MatchString(input.Phone) || (!reportState(input.State) && input.State != "clear") {
			http.Error(w, "Invalid report", 400)
			return
		}
		if input.State == "clear" {
			delete(reports, input.Phone)
		} else {
			if _, ok := reports[input.Phone]; !ok && len(reports) >= 10000 {
				a.json(w, map[string]string{"error": "ظرفیت گزارش‌ها پر شده است"})
				return
			}
			reports[input.Phone] = telegramReport{input.State, time.Now().UnixMilli()}
		}
		path, err := s.file()
		if err == nil {
			err = os.MkdirAll(filepath.Dir(path), 0700)
		}
		if err == nil {
			var f *os.File
			f, err = os.CreateTemp(filepath.Dir(path), ".reports-*.tmp")
			if err == nil {
				tmp := f.Name()
				defer os.Remove(tmp)
				err = json.NewEncoder(f).Encode(reports)
				if err == nil {
					err = f.Sync()
				}
				closeErr := f.Close()
				if err == nil {
					err = closeErr
				}
				if err == nil {
					err = os.Rename(tmp, path)
				}
			}
		}
		if err != nil {
			a.json(w, map[string]string{"error": "ذخیرهٔ گزارش روی دستگاه ممکن نشد"})
			return
		}
	}
	a.json(w, map[string]any{"reports": reports})
}
