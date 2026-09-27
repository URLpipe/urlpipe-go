package urlpipe

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrorKind classifies an [*Error].
type ErrorKind string

// The kinds of error the client returns. Each has a sentinel (ErrXxx) to
// test for with errors.Is.
const (
	KindAuthentication   ErrorKind = "authentication"
	KindEmailUnverified  ErrorKind = "email_unverified"
	KindInvalidRequest   ErrorKind = "invalid_request"
	KindAnalysisFailed   ErrorKind = "analysis_failed"
	KindQuotaExceeded    ErrorKind = "quota_exceeded"
	KindConcurrencyLimit ErrorKind = "concurrency_limit"
	KindRateLimited      ErrorKind = "rate_limited"
	KindNotFound         ErrorKind = "not_found"
	KindStaleResult      ErrorKind = "stale_result"
	KindServer           ErrorKind = "server"
	KindConnection       ErrorKind = "connection"
	KindWaitTimeout      ErrorKind = "wait_timeout"
	// KindUnexpected is an answer the API does not document, such as a 403
	// other than email_unverified or a 429 with an unknown code.
	KindUnexpected ErrorKind = "unexpected"
)

type kindSentinel struct{ kind ErrorKind }

func (s *kindSentinel) Error() string { return "urlpipe: " + string(s.kind) }

// Sentinels to test an error's kind with errors.Is:
//
//	if errors.Is(err, urlpipe.ErrQuotaExceeded) { ... }
var (
	// ErrAuthentication: the API key is missing or not an active project key (401).
	ErrAuthentication error = &kindSentinel{KindAuthentication}
	// ErrEmailUnverified: the key is valid, but the account's email address
	// has not been confirmed yet (403).
	ErrEmailUnverified error = &kindSentinel{KindEmailUnverified}
	// ErrInvalidRequest: a parameter was refused (422 with a code).
	ErrInvalidRequest error = &kindSentinel{KindInvalidRequest}
	// ErrAnalysisFailed: the page could not be analysed (422); the message says why.
	ErrAnalysisFailed error = &kindSentinel{KindAnalysisFailed}
	// ErrQuotaExceeded: the Free plan's monthly credits are spent (429).
	ErrQuotaExceeded error = &kindSentinel{KindQuotaExceeded}
	// ErrConcurrencyLimit: the plan's parallel requests are all running (429).
	ErrConcurrencyLimit error = &kindSentinel{KindConcurrencyLimit}
	// ErrRateLimited: requests are arriving too fast (429).
	ErrRateLimited error = &kindSentinel{KindRateLimited}
	// ErrNotFound: no result for this token under the project (404).
	ErrNotFound error = &kindSentinel{KindNotFound}
	// ErrStaleResult: the result is past the 30-day retention window (410).
	ErrStaleResult error = &kindSentinel{KindStaleResult}
	// ErrServer: the API answered with a 5xx.
	ErrServer error = &kindSentinel{KindServer}
	// ErrConnection: the API could not be reached.
	ErrConnection error = &kindSentinel{KindConnection}
	// ErrWaitTimeout: the analysis was still running when the wait ended.
	ErrWaitTimeout error = &kindSentinel{KindWaitTimeout}
)

var sentinels = map[ErrorKind]error{
	KindAuthentication:   ErrAuthentication,
	KindEmailUnverified:  ErrEmailUnverified,
	KindInvalidRequest:   ErrInvalidRequest,
	KindAnalysisFailed:   ErrAnalysisFailed,
	KindQuotaExceeded:    ErrQuotaExceeded,
	KindConcurrencyLimit: ErrConcurrencyLimit,
	KindRateLimited:      ErrRateLimited,
	KindNotFound:         ErrNotFound,
	KindStaleResult:      ErrStaleResult,
	KindServer:           ErrServer,
	KindConnection:       ErrConnection,
	KindWaitTimeout:      ErrWaitTimeout,
}

// Error is every error the API or the transport produces. Some kinds come
// wrapped in a type with extra fields ([*QuotaExceededError],
// [*ConcurrencyLimitError], [*RateLimitedError], [*AnalysisFailedError]);
// errors.As with an *Error target reaches the Error inside those too.
type Error struct {
	Kind ErrorKind
	// Status is the HTTP status, 0 when there was no response.
	Status int
	// Code is the body's machine-readable error code, such as invalid_url:
	// its error field when that matches ^[a-z][a-z0-9_]*$. Empty when the
	// error is a human sentence.
	Code string
	// Message says what went wrong.
	Message string
	// Body is the response body: decoded JSON (usually map[string]any) or,
	// when it is not JSON, the raw text.
	Body any
	// Token identifies the request, when the answer carried one.
	Token string
	// Err is the underlying cause of a connection error.
	Err error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("urlpipe: ")
	b.WriteString(e.Message)
	if e.Status != 0 || e.Code != "" {
		b.WriteString(" (")
		if e.Status != 0 {
			b.WriteString("HTTP " + strconv.Itoa(e.Status))
		}
		if e.Code != "" {
			if e.Status != 0 {
				b.WriteString(", ")
			}
			b.WriteString(e.Code)
		}
		b.WriteString(")")
	}
	return b.String()
}

// Unwrap returns the underlying cause, if any.
func (e *Error) Unwrap() error { return e.Err }

// Is matches the sentinel of e's kind.
func (e *Error) Is(target error) bool {
	s, ok := sentinels[e.Kind]
	return ok && target == s
}

// base lets the detailed error types embed *Error without the embedded
// field's name (Error) hiding the Error method. Its fields (Status, Code,
// Message, Body, Token) are promoted as usual.
type base = Error

// QuotaExceededError is a 429 quota_exceeded: the Free plan's credits for
// the month are spent. Never retried.
type QuotaExceededError struct {
	*base
	Limit  int
	Used   int
	Needed int
	// ResetsAt is when the allowance rolls over; zero if the API sent none.
	ResetsAt time.Time
}

// Unwrap returns the underlying [*Error].
func (e *QuotaExceededError) Unwrap() error { return e.base }

// ConcurrencyLimitError is a 429 concurrency_limit: the plan's parallel
// requests are all running.
type ConcurrencyLimitError struct {
	*base
	Limit   int
	Running int
}

// Unwrap returns the underlying [*Error].
func (e *ConcurrencyLimitError) Unwrap() error { return e.base }

// RateLimitedError is a 429 rate_limited: requests are arriving too fast.
type RateLimitedError struct {
	*base
	// RetryAfter is how long the API asked to wait; zero when it did not say.
	RetryAfter time.Duration
}

// Unwrap returns the underlying [*Error].
func (e *RateLimitedError) Unwrap() error { return e.base }

// AnalysisFailedError is a 422 where the page could not be analysed; the
// message is the API's sentence saying why.
type AnalysisFailedError struct {
	*base
	// Scrape is set when a /scrape failed in every operation: the body is
	// the scrape object, with each operation's error.
	Scrape *ScrapeResult
}

// Unwrap returns the underlying [*Error].
func (e *AnalysisFailedError) Unwrap() error { return e.base }

// codePattern is what a machine-readable error code looks like; anything
// else in the body's error field is a sentence.
var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var invalidRequestCodes = map[string]bool{
	"invalid_url":             true,
	"invalid_max_age":         true,
	"invalid_options":         true,
	"invalid_labels":          true,
	"invalid_idempotency_key": true,
	"idempotency_key_reused":  true,
}

// apiError classifies a non-success response.
func apiError(status int, header http.Header, raw []byte) error {
	e := &Error{Status: status}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil && obj != nil {
		e.Body = obj
	} else {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			e.Body = v
		} else {
			e.Body = string(raw)
		}
	}
	errField := stringField(obj, "error")
	message := stringField(obj, "message")
	e.Token = stringField(obj, "token")
	if e.Token == "" {
		e.Token = header.Get("X-Result-Token")
	}
	if codePattern.MatchString(errField) {
		e.Code = errField
	}
	// A bare code never becomes the message: each kind below supplies a
	// default sentence instead.
	e.Message = message
	if e.Message == "" && e.Code == "" {
		e.Message = errField
	}

	switch {
	case status == http.StatusUnauthorized:
		e.Kind = KindAuthentication
		e.orMessage("the API key is missing or is not an active project key")
	case status == http.StatusForbidden && errField == "email_unverified":
		e.Kind = KindEmailUnverified
		e.orMessage("confirm the email address on your URLpipe account before using the API")
	case status == http.StatusUnprocessableEntity:
		if invalidRequestCodes[errField] || strings.HasPrefix(errField, "report_to") {
			e.Kind = KindInvalidRequest
			e.orMessage("the API refused a parameter of this request")
			return e
		}
		e.Kind = KindAnalysisFailed
		af := &AnalysisFailedError{base: e}
		if _, ok := obj["operations"]; ok {
			var s ScrapeResult
			if json.Unmarshal(raw, &s) == nil {
				af.Scrape = &s
			}
			if e.Message == "" {
				e.Message = allFailedMessage(&s)
			}
		}
		e.orMessage("the page could not be analysed")
		return af
	case status == http.StatusTooManyRequests:
		switch errField {
		case "quota_exceeded":
			e.Kind = KindQuotaExceeded
			e.orMessage("the monthly credit allowance is spent")
			q := &QuotaExceededError{base: e, Limit: intField(obj, "limit"), Used: intField(obj, "used"), Needed: intField(obj, "needed")}
			if t := parseTime(stringField(obj, "resets_at")); t != nil {
				q.ResetsAt = *t
			}
			return q
		case "concurrency_limit":
			e.Kind = KindConcurrencyLimit
			e.orMessage("every parallel request the plan allows is running")
			return &ConcurrencyLimitError{base: e, Limit: intField(obj, "limit"), Running: intField(obj, "running")}
		case "rate_limited":
			e.Kind = KindRateLimited
			e.orMessage("too many requests; slow down")
			return &RateLimitedError{base: e, RetryAfter: retryAfterOrZero(header, obj)}
		default:
			e.Kind = KindUnexpected
			e.orMessage("unexpected answer: 429 Too Many Requests")
		}
	case status == http.StatusNotFound:
		e.Kind = KindNotFound
		e.orMessage("no result for this token under this project")
	case status == http.StatusGone:
		e.Kind = KindStaleResult
		e.orMessage("this result is older than the 30-day retention window; run a new analysis")
	case status >= 500:
		e.Kind = KindServer
		e.orMessage("the API answered " + strconv.Itoa(status) + " " + http.StatusText(status))
	default:
		e.Kind = KindUnexpected
		e.orMessage("unexpected answer: " + strconv.Itoa(status) + " " + http.StatusText(status))
	}
	return e
}

func (e *Error) orMessage(m string) {
	if e.Message == "" {
		e.Message = m
	}
}

// allFailedMessage lists each operation's error in the body's order.
func allFailedMessage(s *ScrapeResult) string {
	parts := make([]string, 0, len(s.Order))
	for _, op := range s.Order {
		parts = append(parts, string(op)+": "+s.Operations[op].Error)
	}
	return "Every operation failed: " + strings.Join(parts, "; ")
}

func stringField(obj map[string]any, key string) string {
	s, _ := obj[key].(string)
	return s
}

func intField(obj map[string]any, key string) int {
	switch v := obj[key].(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

// retryAfterGiven reads Retry-After (seconds or an HTTP date), falling back
// to the body's retry_after. ok is false when neither was sent.
func retryAfterGiven(h http.Header, obj map[string]any) (time.Duration, bool) {
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 {
			return time.Duration(n * float64(time.Second)), true
		}
		if t, err := http.ParseTime(v); err == nil {
			if d := time.Until(t); d > 0 {
				return d, true
			}
			return 0, true
		}
	}
	if v, ok := obj["retry_after"].(float64); ok && v >= 0 {
		return time.Duration(v * float64(time.Second)), true
	}
	return 0, false
}

func retryAfterOrZero(h http.Header, obj map[string]any) time.Duration {
	d, _ := retryAfterGiven(h, obj)
	return d
}

func connectionError(err error) *Error {
	return &Error{Kind: KindConnection, Message: "could not reach the URLpipe API: " + err.Error(), Err: err}
}

func waitTimeoutError(token string, d time.Duration) *Error {
	return &Error{
		Kind:    KindWaitTimeout,
		Token:   token,
		Message: fmt.Sprintf("the analysis was still running after %s; collect it later with Wait or Result and token %s", d, token),
	}
}
