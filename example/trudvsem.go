package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	serviceURL    = "https://opendata.trudvsem.ru/api/v1/vacancies"
	prefetchCount = 25
)

type (
	trudVsem struct {
		mux         sync.RWMutex
		requestTime time.Time
		client      *http.Client
		vacancies   []Vacancy
	}
	Meta struct {
		Total int `json:"total"`
		Limit int `json:"limit"`
	}
	Region struct {
		RegionCode string `json:"region_code"`
		Name       string `json:"name"`
	}
	Company struct {
		Name string `json:"name"`
	}
	Vacancy struct {
		ID           string  `json:"id"`
		Source       string  `json:"source"`
		Region       Region  `json:"region"`
		Company      Company `json:"company"`
		CreationDate string  `json:"creation-date"`
		SalaryMin    float64 `json:"salary_min"`
		SalaryMax    float64 `json:"salary_max"`
		JobName      string  `json:"job-name"`
		Employment   string  `json:"employment"`
		Schedule     string  `json:"schedule"`
		URL          string  `json:"vac_url"`
	}
	VacancyRec struct {
		Vacancy Vacancy `json:"vacancy"`
	}
	Results struct {
		Vacancies []VacancyRec `json:"vacancies"`
	}
	Response struct {
		Status  string `json:"status"`
		Meta    Meta
		Results Results `json:"results"`
	}
)

func makeReq(ctx context.Context, text string, offset, limit int) (*http.Request, error) {
	URL, err := url.Parse(serviceURL)
	if err != nil {
		return nil, err
	}
	query := url.Values{
		"text":         []string{text},
		"offset":       []string{strconv.Itoa(offset)},
		"limit":        []string{strconv.Itoa(limit)},
		"modifiedFrom": []string{time.Now().Add(-time.Hour * 168).UTC().Format(time.RFC3339)},
	}
	URL.RawQuery = query.Encode()
	return http.NewRequestWithContext(ctx, http.MethodGet, URL.String(), http.NoBody)
}

func (t *trudVsem) loadLastVacancies(ctx context.Context, text string, offset, limit int) ([]VacancyRec, error) {
	req, err := makeReq(ctx, text, offset, limit)
	if err != nil {
		return nil, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	parsed, err := t.parseResponseData(resp)
	return parsed.Results.Vacancies, err
}

func (t *trudVsem) parseResponseData(resp *http.Response) (result Response, err error) {
	dec := json.NewDecoder(resp.Body)
	if err = dec.Decode(&result); err != nil {
		return
	}
	if result.Status != "200" {
		err = errors.New("wrong response status")
		return
	}
	if len(result.Results.Vacancies) == 0 {
		err = io.EOF
	}
	return
}

func (t *trudVsem) refresh() {
	vacancies, err := t.loadLastVacancies(context.Background(), "golang", 0, prefetchCount)
	if err != nil {
		log.Println(err)
		return
	}
	t.mux.Lock()
	t.vacancies = t.vacancies[:0]
	for _, v := range vacancies {
		t.vacancies = append(t.vacancies, v.Vacancy)
	}
	t.mux.Unlock()
}

func (t *trudVsem) Init(context.Context) error {
	rand.Seed(time.Now().UnixNano())
	t.client = http.DefaultClient
	t.vacancies = make([]Vacancy, 0, prefetchCount)
	return nil
}

func (t *trudVsem) Ping(context.Context) error {
	if time.Since(t.requestTime).Minutes() > 1 {
		t.requestTime = time.Now()
		go t.refresh()
	}
	return nil
}

func (t *trudVsem) Close() error {
	return nil
}

func (t *trudVsem) GetRandom() (vacancy Vacancy, ok bool) {
	t.mux.RLock()
	defer t.mux.RUnlock()
	if ok = len(t.vacancies) > 0; !ok {
		return
	}
	vacancy = t.vacancies[rand.Intn(len(t.vacancies))]
	return
}
