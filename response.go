package urlpipe

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Status says where a request stands.
type Status string

// The statuses a [Response] can carry.
const (
	// StatusCompleted means Data holds the result.
	StatusCompleted Status = "completed"
	// StatusAccepted means an async request was accepted; collect the
	// result with the Token.
	StatusAccepted Status = "accepted"
	// StatusProcessing means GET /result/:token found the analysis still
	// running.
	StatusProcessing Status = "processing"
)

// Response is what every method returns. Data is typed by the operation:
// string for Markdown, HTML and Summarize, *Screenshot, *Metadata,
// []string for Keywords, []ConsoleEntry, *Lighthouse and *ScrapeResult.
// Data is only set when Status is [StatusCompleted]; otherwise it is the
// zero value.
type Response[T any] struct {
	Status Status
	Data   T
	// Token identifies the request; GET /result/:token with it is free for
	// 30 days.
	Token string
	// Labels are the labels the request was made with; an empty map when
	// there are none.
	Labels map[string]string
	// Meta is what the response headers said about the request.
	Meta Meta
}

// Completed reports whether Data holds the result.
func (r *Response[T]) Completed() bool { return r.Status == StatusCompleted }

// Meta is the request metadata the API sends in its X- headers (and, on a
// webhook, in the payload's meta object). A value the API did not send is
// nil, or "" for strings.
type Meta struct {
	// Cache is "hit", "miss" or, for a scrape, "partial".
	Cache string
	// CacheAge is the served result's age in seconds, on a hit.
	CacheAge *int
	// ProcessingTimeMs is the total processing time, once the work is done.
	ProcessingTimeMs *int
	Quota            Quota
	// ConcurrencyLimit is how many requests the plan runs in parallel.
	ConcurrencyLimit *Amount
	// ResultURL is a link to a screenshot that needs no API key.
	ResultURL string
	// IdempotentReplayed is true when this answer belongs to an earlier
	// request with the same Idempotency-Key.
	IdempotentReplayed bool
}

// Quota is what a request cost and what is left of the monthly allowance.
type Quota struct {
	Cost      *int
	Limit     *Amount
	Remaining *Amount
	Overage   *int
	ResetsAt  *time.Time
}

// Amount is a quota figure that is either a number or "unlimited".
type Amount struct {
	// Value is the number, when there is one.
	Value int
	// Unlimited is true on an unlimited plan.
	Unlimited bool
	// Raw is the value exactly as the API sent it.
	Raw string
}

func (a *Amount) String() string {
	if a == nil {
		return ""
	}
	return a.Raw
}

func parseAmount(s string) *Amount {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	a := &Amount{Raw: s}
	if strings.EqualFold(s, "unlimited") {
		a.Unlimited = true
	} else if n, err := strconv.Atoi(s); err == nil {
		a.Value = n
	}
	return a
}

// UnmarshalJSON reads a number or a string such as "unlimited".
func (a *Amount) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		s = string(b)
	}
	if p := parseAmount(s); p != nil {
		*a = *p
	}
	return nil
}

func parseInt(s string) *int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	return &n
}

func parseTime(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	return &t
}

func parseMeta(h http.Header) Meta {
	return Meta{
		Cache:            h.Get("X-Cache"),
		CacheAge:         parseInt(h.Get("X-Cache-Age")),
		ProcessingTimeMs: parseInt(h.Get("X-Processing-Time-Ms")),
		Quota: Quota{
			Cost:      parseInt(h.Get("X-Quota-Cost")),
			Limit:     parseAmount(h.Get("X-Quota-Limit")),
			Remaining: parseAmount(h.Get("X-Quota-Remaining")),
			Overage:   parseInt(h.Get("X-Quota-Overage")),
			ResetsAt:  parseTime(h.Get("X-Quota-Reset")),
		},
		ConcurrencyLimit:   parseAmount(h.Get("X-Concurrency-Limit")),
		ResultURL:          h.Get("X-Result-Url"),
		IdempotentReplayed: strings.EqualFold(strings.TrimSpace(h.Get("Idempotent-Replayed")), "true"),
	}
}

// parseLabels reads X-Labels; no labels is an empty map, never nil.
func parseLabels(s string) map[string]string {
	labels := map[string]string{}
	if s != "" {
		_ = json.Unmarshal([]byte(s), &labels)
	}
	if labels == nil {
		labels = map[string]string{}
	}
	return labels
}

// UnmarshalJSON reads the meta object of a webhook payload, where the
// concurrency limit sits inside quota.
func (m *Meta) UnmarshalJSON(b []byte) error {
	var aux struct {
		Cache            *string `json:"cache"`
		CacheAge         *int    `json:"cache_age"`
		ProcessingTimeMs *int    `json:"processing_time_ms"`
		Quota            *struct {
			Cost             *int    `json:"cost"`
			Limit            *Amount `json:"limit"`
			Remaining        *Amount `json:"remaining"`
			Overage          *int    `json:"overage"`
			ResetsAt         *string `json:"resets_at"`
			ConcurrencyLimit *Amount `json:"concurrency_limit"`
		} `json:"quota"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	*m = Meta{CacheAge: aux.CacheAge, ProcessingTimeMs: aux.ProcessingTimeMs}
	if aux.Cache != nil {
		m.Cache = *aux.Cache
	}
	if q := aux.Quota; q != nil {
		m.Quota = Quota{Cost: q.Cost, Limit: q.Limit, Remaining: q.Remaining, Overage: q.Overage}
		if q.ResetsAt != nil {
			m.Quota.ResetsAt = parseTime(*q.ResetsAt)
		}
		m.ConcurrencyLimit = q.ConcurrencyLimit
	}
	return nil
}

// typed narrows a Response[any] to the type its operation produces.
func typed[T any](r *Response[any]) *Response[T] {
	out := &Response[T]{Status: r.Status, Token: r.Token, Labels: r.Labels, Meta: r.Meta}
	if d, ok := r.Data.(T); ok {
		out.Data = d
	}
	return out
}
