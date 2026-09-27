package urlpipe

import (
	"errors"
	"net/http"
	"regexp"
	"testing"
	"time"
)

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestRetryAfter503ReusesTheIdempotencyKey(t *testing.T) {
	s, c := newStub(t, jsonReply(503, `{"error":"unavailable"}`), text(200, "# Hi"))
	r, err := c.Markdown(ctx, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Data != "# Hi" {
		t.Errorf("data = %q", r.Data)
	}
	reqs := s.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	first, second := reqs[0].Header.Get("Idempotency-Key"), reqs[1].Header.Get("Idempotency-Key")
	if !uuidV4.MatchString(first) {
		t.Errorf("generated key %q is not a UUID v4", first)
	}
	if first != second {
		t.Errorf("retry changed the key: %q then %q", first, second)
	}
}

func TestEachCallGetsItsOwnKey(t *testing.T) {
	s, c := newStub(t, text(200, "ok"))
	for i := 0; i < 2; i++ {
		if _, err := c.Markdown(ctx, "https://example.com", nil); err != nil {
			t.Fatal(err)
		}
	}
	reqs := s.requests()
	if reqs[0].Header.Get("Idempotency-Key") == reqs[1].Header.Get("Idempotency-Key") {
		t.Error("two calls shared an Idempotency-Key")
	}
}

func TestTheCallersKeyIsUsedForRetries(t *testing.T) {
	s, c := newStub(t, jsonReply(502, ``), jsonReply(500, ``), text(200, "ok"))
	if _, err := c.Markdown(ctx, "https://example.com", &Options{IdempotencyKey: "mine"}); err != nil {
		t.Fatal(err)
	}
	for _, req := range s.requests() {
		if got := req.Header.Get("Idempotency-Key"); got != "mine" {
			t.Errorf("Idempotency-Key = %q", got)
		}
	}
}

func TestNoGeneratedKeyWithoutRetries(t *testing.T) {
	s, c := newStub(t, text(200, "ok"))
	c.maxRetries = 0
	if _, err := c.Markdown(ctx, "https://example.com", nil); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t).Header.Get("Idempotency-Key"); got != "" {
		t.Errorf("Idempotency-Key = %q, want none", got)
	}
}

func TestRetriesStopAtMaxRetries(t *testing.T) {
	s, c := newStub(t, jsonReply(503, `{"error":"unavailable"}`))
	_, err := c.Markdown(ctx, "https://example.com", nil)
	if !errors.Is(err, ErrServer) {
		t.Fatalf("err = %v", err)
	}
	if n := len(s.requests()); n != 3 {
		t.Errorf("attempts = %d, want 3", n)
	}
}

func TestNotRetried(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{401, `{"error":"unauthorized"}`},
		{403, `{"error":"email_unverified"}`},
		{404, `{"error":"not_found"}`},
		{410, `{"error":"stale"}`},
		{422, `{"error":"invalid_url"}`},
		{422, `{"error":"The request timed out."}`},
		{429, `{"error":"quota_exceeded","limit":1000,"used":1000,"needed":1}`},
		{429, `{"error":"something_else"}`},
		{429, ``},
		{504, `{"error":"gateway_timeout"}`},
	} {
		s, c := newStub(t, jsonReply(tc.status, tc.body), text(200, "ok"))
		if _, err := c.Markdown(ctx, "https://example.com", nil); err == nil {
			t.Errorf("%d %s: no error", tc.status, tc.body)
		}
		if n := len(s.requests()); n != 1 {
			t.Errorf("%d %s was retried: %d requests", tc.status, tc.body, n)
		}
	}
}

func TestRateLimitedHonoursRetryAfter(t *testing.T) {
	s, c := newStub(t,
		jsonReply(429, `{"error":"rate_limited","retry_after":0}`, "Retry-After", "0"),
		text(200, "ok"),
	)
	c.backoffBase = time.Hour // proves Retry-After, not the backoff, set the pause
	start := time.Now()
	r, err := c.Markdown(ctx, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Data != "ok" || len(s.requests()) != 2 {
		t.Errorf("data %q after %d requests", r.Data, len(s.requests()))
	}
	if time.Since(start) > 5*time.Second {
		t.Error("Retry-After: 0 was not honoured")
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	c := testClient(t, "http://127.0.0.1")
	res := &rawResponse{status: 429, header: http.Header{"Retry-After": {"3600"}}, body: []byte(`{"error":"rate_limited"}`)}
	d, retry := c.retryDelay(res, 0)
	if !retry || d != 60*time.Second {
		t.Errorf("delay = %v %v, want 60s", d, retry)
	}
}

func TestConcurrencyLimitBacksOffExponentially(t *testing.T) {
	c := testClient(t, "http://127.0.0.1")
	c.backoffBase = time.Second
	res := &rawResponse{status: 429, header: http.Header{}, body: []byte(`{"error":"concurrency_limit","limit":1,"running":1}`)}
	for attempt, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		if d, retry := c.retryDelay(res, attempt); !retry || d != want {
			t.Errorf("attempt %d: delay %v %v, want %v", attempt, d, retry, want)
		}
	}
}

func TestConcurrencyLimitIsRetried(t *testing.T) {
	s, c := newStub(t, jsonReply(429, `{"error":"concurrency_limit","limit":1,"running":1}`), text(200, "ok"))
	if _, err := c.Markdown(ctx, "https://example.com", nil); err != nil {
		t.Fatal(err)
	}
	if n := len(s.requests()); n != 2 {
		t.Errorf("requests = %d, want 2", n)
	}
}

func TestConnectionErrorsAreRetried(t *testing.T) {
	s, c := newStub(t, hangUp(t), text(200, "ok"))
	r, err := c.Markdown(ctx, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if r.Data != "ok" || len(reqs) != 2 || reqs[0].Header.Get("Idempotency-Key") != reqs[1].Header.Get("Idempotency-Key") {
		t.Errorf("data %q, %d requests", r.Data, len(reqs))
	}
}

func TestRateLimitedWithoutRetryAfterWaitsOneSecond(t *testing.T) {
	c := testClient(t, "http://127.0.0.1")
	c.backoffBase = time.Second
	res := &rawResponse{status: 429, header: http.Header{}, body: []byte(`{"error":"rate_limited"}`)}
	if d, retry := c.retryDelay(res, 1); !retry || d != time.Second {
		t.Errorf("delay = %v %v, want 1s", d, retry)
	}
	res.body = []byte(`{"error":"rate_limited","retry_after":7}`)
	if d, _ := c.retryDelay(res, 0); d != 7*time.Second {
		t.Errorf("body retry_after: delay = %v, want 7s", d)
	}
}

func TestBackoffIsCappedAtAMinute(t *testing.T) {
	c := testClient(t, "http://127.0.0.1")
	c.backoffBase = time.Second
	for _, attempt := range []int{6, 10, 100} {
		if d := c.backoff(attempt); d != 60*time.Second {
			t.Errorf("attempt %d: backoff %v, want 60s", attempt, d)
		}
	}
	if d := c.backoff(5); d != 32*time.Second {
		t.Errorf("attempt 5: backoff %v, want 32s", d)
	}
}
