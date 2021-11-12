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
		mux         sync.Mutex
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

func (t *trudVsem) getData(ctx context.Context, text string, offset, limit int) error {
	req, err := makeReq(ctx, text, offset, limit)
	if err != nil {
		return err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return t.parseResponseData(resp)
}

func (t *trudVsem) parseResponseData(resp *http.Response) error {
	dec := json.NewDecoder(resp.Body)
	var result Response
	if err := dec.Decode(&result); err != nil {
		return err
	}
	if result.Status != "200" {
		return errors.New("wrong response status")
	}
	if len(result.Results.Vacancies) == 0 {
		return io.EOF
	}
	t.mux.Lock()
	t.vacancies = t.vacancies[:0]
	for _, v := range result.Results.Vacancies {
		t.vacancies = append(t.vacancies, v.Vacancy)
	}
	t.mux.Unlock()
	return nil
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

func (t *trudVsem) refresh() {
	if err := t.getData(context.Background(), "golang", 0, prefetchCount); err != nil {
		log.Println(err)
	}
}

func (t *trudVsem) GetRandom() (vacancy Vacancy, ok bool) {
	t.mux.Lock()
	defer t.mux.Unlock()
	if ok = len(t.vacancies) > 0; !ok {
		return
	}
	vacancy = t.vacancies[rand.Intn(len(t.vacancies))]
	return
}
