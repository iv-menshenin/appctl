package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	VacancyRender []byte
	trudVsem      struct {
		mux         sync.RWMutex
		requestTime time.Time
		client      *http.Client
		vacancies   []VacancyRender
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
		Status  string  `json:"status"`
		Meta    Meta    `json:"meta"`
		Results Results `json:"results"`
	}
)

func (t *trudVsem) Init(context.Context) error {
	t.client = http.DefaultClient
	t.vacancies = make([]VacancyRender, 0, prefetchCount)
	return nil
}

func (t *trudVsem) Ping(context.Context) error {
	if time.Since(t.requestTime).Minutes() > 1 {
		t.requestTime = time.Now()
		go t.refresh()
	}
	return nil
}

func (t *trudVsem) Ident() string {
	return "trudvsem.ru"
}

func (t *trudVsem) Close() error {
	return nil
}

func (t *trudVsem) GetRandomVacancy() (vacancy VacancyRender, ok bool) {
	t.mux.RLock()
	defer t.mux.RUnlock()
	if ok = len(t.vacancies) > 0; !ok {
		return
	}
	vacancy = t.vacancies[rand.Intn(len(t.vacancies))]
	return
}

func (t *trudVsem) refresh() {
	vacancies, err := t.loadLastVacancies(context.Background(), "программист", 0, prefetchCount)
	if err != nil {
		log.Println(err)
		return
	}
	t.mux.Lock()
	t.vacancies = t.vacancies[:0]
	for _, v := range vacancies {
		var newVacancy = v.Vacancy
		if rendered, err := newVacancy.renderBytes(); err == nil {
			t.vacancies = append(t.vacancies, rendered)
		}
	}
	t.mux.Unlock()
}

func (t *trudVsem) loadLastVacancies(ctx context.Context, text string, offset, limit int) ([]VacancyRec, error) {
	var weeks = 1
	for {
		req, err := newVacanciesRequest(ctx, text, offset, limit, weeks)
		if err != nil {
			return nil, err
		}
		parsed, err := t.executeRequest(req)
		if errors.Is(err, ErrEmpty) {
			<-time.After(5 * time.Second)
			weeks++
			continue
		}
		return parsed.Results.Vacancies, err
	}
}

func (t *trudVsem) executeRequest(req *http.Request) (result Response, err error) {
	resp, err := t.client.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	return parseResponseData(resp)
}

func newVacanciesRequest(ctx context.Context, text string, offset, limit, weeks int) (*http.Request, error) {
	URL, err := url.ParseRequestURI(serviceURL)
	if err != nil {
		return nil, err
	}
	query := url.Values{
		"text":         []string{text},
		"offset":       []string{strconv.Itoa(offset)},
		"limit":        []string{strconv.Itoa(limit)},
		"modifiedFrom": []string{modifiedFrom(weeks)},
	}
	URL.RawQuery = query.Encode()
	return http.NewRequestWithContext(ctx, http.MethodGet, URL.String(), http.NoBody)
}

const hoursInWeek = 168

func modifiedFrom(weeks int) string {
	return time.Now().Add(-time.Hour * hoursInWeek * time.Duration(weeks)).UTC().Format(time.RFC3339)
}

func parseResponseData(resp *http.Response) (result Response, err error) {
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return
	}
	if result.Status != "200" {
		err = errors.New("wrong response status")
		return
	}
	if len(result.Results.Vacancies) == 0 {
		err = ErrEmpty
	}
	return
}

var ErrEmpty = errors.New("empty")

func (v Vacancy) renderBytes() ([]byte, error) {
	var w = bytes.NewBufferString("")
	if err := v.renderHead(w); err != nil {
		return nil, err
	}
	if err := v.renderDesc(w); err != nil {
		return nil, err
	}
	if err := v.renderConditions(w); err != nil {
		return nil, err
	}
	if err := v.renderSalary(w); err != nil {
		return nil, err
	}
	if err := v.renderFooter(w); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

func (v Vacancy) renderHead(w io.StringWriter) error {
	_, err := w.WriteString(fmt.Sprintf("<h3>%s (%s)</h3>", v.JobName, v.Region.Name))
	return err
}

func (v Vacancy) renderDesc(w io.StringWriter) error {
	_, err := w.WriteString(fmt.Sprintf("<p class='description'>Компания: %s ищет сотрудника на должность '%s'.</p>", v.Company.Name, v.JobName))
	return err
}

func (v Vacancy) renderConditions(w io.StringWriter) error {
	_, err := w.WriteString(fmt.Sprintf("<p class='condition'>Условия: %s, %s.</p>", v.Employment, v.Schedule))
	return err
}

func (v Vacancy) renderSalary(w io.StringWriter) error {
	if v.SalaryMin != v.SalaryMax && v.SalaryMax > 0 && v.SalaryMin > 0 {
		_, err := w.WriteString(fmt.Sprintf("<p class='salary'>зарплата от %0.2f до %0.2f руб.</p>", v.SalaryMin, v.SalaryMax))
		return err
	}
	if v.SalaryMax > 0 {
		_, err := w.WriteString(fmt.Sprintf("<p class='salary'>зарплата %0.2f руб.</p>", v.SalaryMax))
		return err
	}
	if v.SalaryMin > 0 {
		_, err := w.WriteString(fmt.Sprintf("<p class='salary'>зарплата %0.2f руб.</p>", v.SalaryMin))
		return err
	}
	return nil
}

func (v Vacancy) renderFooter(w io.StringWriter) error {
	_, err := w.WriteString(fmt.Sprintf("<a href='%s'>ознакомиться</a>", v.URL))
	return err
}

func (r VacancyRender) RenderTo(w io.Writer) error {
	_, err := w.Write(r)
	return err
}
