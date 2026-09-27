package urlpipe

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestErrorTable(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		headers  []string
		sentinel error
		kind     ErrorKind
		code     string
		message  string
		token    string
	}{
		{"401", 401, `{"error":"unauthorized"}`, nil, ErrAuthentication, KindAuthentication, "unauthorized", "the API key is missing or is not an active project key", ""},
		{"401 without a body", 401, ``, nil, ErrAuthentication, KindAuthentication, "", "the API key is missing or is not an active project key", ""},
		{"403 email_unverified", 403, `{"error":"email_unverified","message":"Confirm the email address on this account before using the API."}`, nil,
			ErrEmailUnverified, KindEmailUnverified, "email_unverified", "Confirm the email address on this account before using the API.", ""},
		{"403 email_unverified without a message", 403, `{"error":"email_unverified"}`, nil,
			ErrEmailUnverified, KindEmailUnverified, "email_unverified", "confirm the email address on your URLpipe account before using the API", ""},
		{"403 email_unverified without a body", 403, ``, nil,
			nil, KindUnexpected, "", "unexpected answer: 403 Forbidden", ""},
		{"403 other", 403, `{"error":"forbidden","message":"Nope."}`, nil, nil, KindUnexpected, "forbidden", "Nope.", ""},
		{"422 invalid_url", 422, `{"error":"invalid_url","message":"The url must be a public http(s) URL."}`, nil, ErrInvalidRequest, KindInvalidRequest, "invalid_url", "The url must be a public http(s) URL.", ""},
		{"422 invalid_max_age", 422, `{"error":"invalid_max_age"}`, nil, ErrInvalidRequest, KindInvalidRequest, "invalid_max_age", "the API refused a parameter of this request", ""},
		{"422 invalid_options", 422, `{"error":"invalid_options","message":"screenshot_options.viewport_width must be a whole number from 320 to 1920."}`, nil, ErrInvalidRequest, KindInvalidRequest, "invalid_options", "screenshot_options.viewport_width must be a whole number from 320 to 1920.", ""},
		{"422 invalid_labels", 422, `{"error":"invalid_labels","message":"labels.client must be a string."}`, nil, ErrInvalidRequest, KindInvalidRequest, "invalid_labels", "labels.client must be a string.", ""},
		{"422 invalid_idempotency_key", 422, `{"error":"invalid_idempotency_key"}`, nil, ErrInvalidRequest, KindInvalidRequest, "invalid_idempotency_key", "the API refused a parameter of this request", ""},
		{"422 idempotency_key_reused", 422, `{"error":"idempotency_key_reused","message":"This Idempotency-Key was sent with a different request."}`, nil, ErrInvalidRequest, KindInvalidRequest, "idempotency_key_reused", "This Idempotency-Key was sent with a different request.", ""},
		{"422 report_to", 422, `{"error":"report_to must not be an IP address."}`, nil, ErrInvalidRequest, KindInvalidRequest, "", "report_to must not be an IP address.", ""},
		{"422 analysis failed with a message", 422, `{"error":"The page could not be loaded.","message":"More detail."}`, nil, ErrAnalysisFailed, KindAnalysisFailed, "", "More detail.", ""},
		{"422 analysis failed", 422, `{"error":"The request timed out."}`, []string{"X-Result-Token", "tok_failed"}, ErrAnalysisFailed, KindAnalysisFailed, "", "The request timed out.", "tok_failed"},
		{"404 not_found", 404, `{"error":"not_found"}`, nil, ErrNotFound, KindNotFound, "not_found", "no result for this token under this project", ""},
		{"404 whatever the body", 404, `<html>gone</html>`, nil, ErrNotFound, KindNotFound, "", "no result for this token under this project", ""},
		{"410 whatever the body", 410, ``, nil, ErrStaleResult, KindStaleResult, "", "this result is older than the 30-day retention window; run a new analysis", ""},
		{"429 unknown code", 429, `{"error":"slow_down","message":"Later."}`, nil, nil, KindUnexpected, "slow_down", "Later.", ""},
		{"429 without a code", 429, `{}`, nil, nil, KindUnexpected, "", "unexpected answer: 429 Too Many Requests", ""},
		{"token from the body wins", 422, `{"error":"invalid_url","token":"tok_body"}`, []string{"X-Result-Token", "tok_header"}, ErrInvalidRequest, KindInvalidRequest, "invalid_url", "the API refused a parameter of this request", "tok_body"},
		{"410 stale", 410, `{"error":"stale","message":"This result is older than the 30-day retention window. Run a new analysis."}`, nil, ErrStaleResult, KindStaleResult, "stale", "This result is older than the 30-day retention window. Run a new analysis.", ""},
		{"503 bare code", 503, `{"error":"maintenance"}`, nil, ErrServer, KindServer, "maintenance", "the API answered 503 Service Unavailable", ""},
		{"500", 500, `oops`, nil, ErrServer, KindServer, "", "the API answered 500 Internal Server Error", ""},
		{"503 json", 503, `{"error":"maintenance","message":"Back soon."}`, nil, ErrServer, KindServer, "maintenance", "Back soon.", ""},
		{"504 without a token", 504, `{"error":"gateway_timeout"}`, nil, ErrServer, KindServer, "gateway_timeout", "the API answered 504 Gateway Timeout", ""},
		{"418", 418, `short and stout`, nil, nil, KindUnexpected, "", "unexpected answer: 418 I'm a teapot", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, c := newStub(t, jsonReply(tc.status, tc.body, tc.headers...))
			_, err := c.Markdown(ctx, "https://example.com", nil)
			if err == nil {
				t.Fatal("no error")
			}
			if tc.sentinel != nil && !errors.Is(err, tc.sentinel) {
				t.Errorf("errors.Is(%v, %v) = false", err, tc.sentinel)
			}
			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("errors.As(*Error) failed for %T", err)
			}
			if e.Kind != tc.kind || e.Status != tc.status || e.Code != tc.code || e.Message != tc.message || e.Token != tc.token {
				t.Errorf("error = kind %q status %d code %q message %q token %q", e.Kind, e.Status, e.Code, e.Message, e.Token)
			}
			if e.Body == nil {
				t.Error("Body is nil")
			}
		})
	}
}

func TestErrorKindsDoNotCrossMatch(t *testing.T) {
	_, c := newStub(t, jsonReply(401, `{"error":"unauthorized"}`))
	_, err := c.Markdown(ctx, "https://example.com", nil)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrServer) {
		t.Errorf("401 matched another kind: %v", err)
	}
}

func TestErrorBodyIsKept(t *testing.T) {
	_, c := newStub(t, jsonReply(500, `not json at all`))
	c.maxRetries = 0
	_, err := c.HTML(ctx, "https://example.com", nil)
	var e *Error
	if !errors.As(err, &e) || e.Body != "not json at all" {
		t.Fatalf("body = %#v", e.Body)
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("message = %q", err.Error())
	}
}

func TestQuotaExceeded(t *testing.T) {
	s, c := newStub(t, jsonReply(429, `{"error":"quota_exceeded","message":"Your Free plan includes 1000 credits per month.","limit":1000,"used":990,"needed":17,"resets_at":"2026-10-01T00:00:00Z"}`,
		"X-Quota-Cost", "17"))
	_, err := c.Summarize(ctx, "https://example.com", nil)
	var q *QuotaExceededError
	if !errors.As(err, &q) {
		t.Fatalf("err = %T %v", err, err)
	}
	if q.Limit != 1000 || q.Used != 990 || q.Needed != 17 || !q.ResetsAt.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("quota error = %+v", q)
	}
	if q.Status != 429 || q.Code != "quota_exceeded" || q.Message != "Your Free plan includes 1000 credits per month." {
		t.Errorf("base = %+v", q.base)
	}
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Error("errors.Is(ErrQuotaExceeded) = false")
	}
	if n := len(s.requests()); n != 1 {
		t.Errorf("quota_exceeded was retried: %d requests", n)
	}
}

func TestConcurrencyLimit(t *testing.T) {
	_, c := newStub(t, jsonReply(429, `{"error":"concurrency_limit","message":"3 are already running.","limit":3,"running":3}`))
	_, err := c.Markdown(ctx, "https://example.com", nil)
	var cl *ConcurrencyLimitError
	if !errors.As(err, &cl) || cl.Limit != 3 || cl.Running != 3 || !errors.Is(err, ErrConcurrencyLimit) {
		t.Fatalf("err = %T %+v", err, err)
	}
}

func TestRateLimited(t *testing.T) {
	_, c := newStub(t, jsonReply(429, `{"error":"rate_limited","message":"Too many requests.","retry_after":12}`, "Retry-After", "0"))
	c.maxRetries = 0
	_, err := c.Markdown(ctx, "https://example.com", nil)
	var rl *RateLimitedError
	if !errors.As(err, &rl) || !errors.Is(err, ErrRateLimited) || rl.RetryAfter != 0 {
		t.Fatalf("err = %T %+v", err, err)
	}

	_, c = newStub(t, jsonReply(429, `{"error":"rate_limited","retry_after":12}`))
	c.maxRetries = 0
	_, err = c.Markdown(ctx, "https://example.com", nil)
	if !errors.As(err, &rl) || rl.RetryAfter != 12*time.Second {
		t.Fatalf("retry after from the body = %v", err)
	}
}

func TestScrapeWhereEveryOperationFailed(t *testing.T) {
	_, c := newStub(t, jsonReply(422, `{"url":"https://example.com","operations":{
		"meta":{"success":false,"result":null,"error":"The requested page was not found.","cached":false},
		"markdown":{"success":false,"result":null,"error":"The request timed out.","cached":false}}}`))
	_, err := c.Scrape(ctx, "https://example.com", []Operation{OperationMarkdown, OperationMeta}, nil)
	var af *AnalysisFailedError
	if !errors.As(err, &af) || !errors.Is(err, ErrAnalysisFailed) {
		t.Fatalf("err = %T %v", err, err)
	}
	if af.Scrape == nil || af.Scrape.Operations[OperationMeta].Error != "The requested page was not found." ||
		!reflect.DeepEqual(af.Scrape.Order, []Operation{OperationMeta, OperationMarkdown}) {
		t.Errorf("scrape = %+v", af.Scrape)
	}
	body, ok := af.Body.(map[string]any)
	if !ok || body["operations"] == nil {
		t.Errorf("body = %#v", af.Body)
	}
	if af.Code != "" || af.Message != "Every operation failed: meta: The requested page was not found.; markdown: The request timed out." {
		t.Errorf("message = %q", af.Message)
	}
}

func TestConnectionError(t *testing.T) {
	s, c := newStub(t, hangUp(t))
	_, err := c.Markdown(ctx, "https://example.com", nil)
	var e *Error
	if !errors.Is(err, ErrConnection) || !errors.As(err, &e) || e.Err == nil || e.Status != 0 {
		t.Fatalf("err = %v", err)
	}
	if n := len(s.requests()); n != 3 {
		t.Errorf("attempts = %d, want 3 (1 + 2 retries)", n)
	}
}
