package urlpipe

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var ctx = context.Background()

func TestNewClientReadsTheEnvironment(t *testing.T) {
	t.Setenv(APIKeyEnv, "env-key")
	s, _ := newStub(t, text(200, "# Hi"))
	c, err := NewClient(WithBaseURL(s.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Markdown(ctx, "https://example.com", nil); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t).Header.Get("Authorization"); got != "Bearer env-key" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestNewClientPrefersTheExplicitKey(t *testing.T) {
	t.Setenv(APIKeyEnv, "env-key")
	c, err := NewClient(WithAPIKey("explicit"))
	if err != nil {
		t.Fatal(err)
	}
	if c.apiKey != "explicit" {
		t.Errorf("apiKey = %q", c.apiKey)
	}
}

func TestNewClientWithoutAKey(t *testing.T) {
	t.Setenv(APIKeyEnv, "")
	_, err := NewClient()
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("err = %v, want ErrMissingAPIKey", err)
	}
}

func TestNewClientDefaults(t *testing.T) {
	c, err := NewClient(WithAPIKey("k"))
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != DefaultBaseURL || c.timeout != 90*time.Second || c.maxRetries != 2 || c.waitTimeout != 300*time.Second {
		t.Errorf("defaults = %q %v %d %v", c.baseURL, c.timeout, c.maxRetries, c.waitTimeout)
	}
}

// Every operation: path, body, auth, User-Agent, sync default and typed data.
func TestOperations(t *testing.T) {
	lighthouseJSON := `{"url":"https://example.com","fetchTime":"2026-01-01T00:00:00.000Z","device":"desktop",
		"categories":{"performance":{"score":0.95,"title":"Performance"},"pwa":null},
		"metrics":{"first-contentful-paint":{"score":0.95,"displayValue":"1.2 s","numericValue":1200,"numericUnit":"millisecond"},"largest-contentful-paint":null},
		"audits":{"image-alt":{"score":1}},"extra":"kept"}`
	score := 0.95
	fcp := 1200.0

	cases := []struct {
		name     string
		path     string
		reply    http.HandlerFunc
		call     func(c *Client) (any, error)
		want     any
		wantBody map[string]any
	}{
		{
			name:  "markdown",
			path:  "/markdown",
			reply: text(200, "# Example Domain"),
			call: func(c *Client) (any, error) {
				r, err := c.Markdown(ctx, "https://example.com", nil)
				return data(r, err)
			},
			want:     "# Example Domain",
			wantBody: map[string]any{"url": "https://example.com", "sync": true},
		},
		{
			name:  "html",
			path:  "/html",
			reply: text(200, "<html></html>"),
			call: func(c *Client) (any, error) {
				r, err := c.HTML(ctx, "https://example.com", nil)
				return data(r, err)
			},
			want:     "<html></html>",
			wantBody: map[string]any{"url": "https://example.com", "sync": true},
		},
		{
			name:  "summarize",
			path:  "/summarize",
			reply: text(200, "A **short** summary."),
			call: func(c *Client) (any, error) {
				r, err := c.Summarize(ctx, "https://example.com", nil)
				return data(r, err)
			},
			want:     "A **short** summary.",
			wantBody: map[string]any{"url": "https://example.com", "sync": true},
		},
		{
			name:  "meta",
			path:  "/meta",
			reply: jsonOK(`{"title":"Example Domain","description":"Examples.","language":"en","main_image_url":null,"favicon_url":"https://example.com/favicon.ico","author_name":"Jane Doe","feed_url":null,"publication_date":"2026-01-01T00:00:00Z","additional_author_information":{"twitter":"@janedoe"}}`),
			call: func(c *Client) (any, error) {
				r, err := c.Meta(ctx, "https://example.com", nil)
				return data(r, err)
			},
			want: &Metadata{
				Title: "Example Domain", Description: "Examples.", Language: "en",
				FaviconURL: "https://example.com/favicon.ico", AuthorName: "Jane Doe",
				PublicationDate:             "2026-01-01T00:00:00Z",
				AdditionalAuthorInformation: map[string]any{"twitter": "@janedoe"},
			},
			wantBody: map[string]any{"url": "https://example.com", "sync": true},
		},
		{
			name:  "keywords",
			path:  "/keywords",
			reply: jsonOK(`["example domain","IANA"]`),
			call: func(c *Client) (any, error) {
				r, err := c.Keywords(ctx, "https://example.com", nil)
				return data(r, err)
			},
			want:     []string{"example domain", "IANA"},
			wantBody: map[string]any{"url": "https://example.com", "sync": true},
		},
		{
			name:  "console",
			path:  "/console",
			reply: jsonOK(`[{"type":"error","text":"boom"},{"type":"exception","text":"ReferenceError: bar is not defined"}]`),
			call: func(c *Client) (any, error) {
				r, err := c.Console(ctx, "https://example.com", nil)
				return data(r, err)
			},
			want:     []ConsoleEntry{{Type: ConsoleError, Text: "boom"}, {Type: ConsoleException, Text: "ReferenceError: bar is not defined"}},
			wantBody: map[string]any{"url": "https://example.com", "sync": true},
		},
		{
			name:  "lighthouse",
			path:  "/lighthouse",
			reply: jsonOK(lighthouseJSON),
			call: func(c *Client) (any, error) {
				r, err := c.Lighthouse(ctx, "https://example.com", &LighthouseOptions{Device: "desktop", IncludeAudits: true})
				if err != nil {
					return nil, err
				}
				l := r.Data
				l.Raw = nil
				return l, nil
			},
			want: &Lighthouse{
				URL: "https://example.com", FetchTime: "2026-01-01T00:00:00.000Z", Device: "desktop",
				Categories: map[string]*LighthouseCategory{"performance": {Score: &score, Title: "Performance"}, "pwa": nil},
				Metrics: map[string]*LighthouseMetric{
					"first-contentful-paint":   {Score: &score, DisplayValue: "1.2 s", NumericValue: &fcp, NumericUnit: "millisecond"},
					"largest-contentful-paint": nil,
				},
				Audits: map[string]json.RawMessage{"image-alt": json.RawMessage(`{"score":1}`)},
			},
			wantBody: map[string]any{"url": "https://example.com", "sync": true, "device": "desktop", "include_audits": true},
		},
		{
			name:  "scrape",
			path:  "/scrape",
			reply: jsonOK(`{"url":"https://example.com","operations":{"markdown":{"success":true,"result":"# Hi","cached":false}}}`),
			call: func(c *Client) (any, error) {
				r, err := c.Scrape(ctx, "https://example.com", []Operation{OperationMarkdown, OperationLighthouse}, &ScrapeOptions{Device: "mobile", IncludeAudits: true, Screenshot: map[string]any{"format": "webp"}})
				if err != nil {
					return nil, err
				}
				return r.Data.Markdown()
			},
			want: "# Hi",
			wantBody: map[string]any{
				"url": "https://example.com", "sync": true, "operations": []any{"markdown", "lighthouse"},
				"device": "mobile", "include_audits": true, "screenshot_options": map[string]any{"format": "webp"},
			},
		},
		{
			name:  "screenshot",
			path:  "/screenshot",
			reply: text(200, base64.StdEncoding.EncodeToString(pngBytes)),
			call: func(c *Client) (any, error) {
				r, err := c.Screenshot(ctx, "https://example.com", &ScreenshotOptions{Screenshot: map[string]any{"full_page": false}})
				if err != nil {
					return nil, err
				}
				return r.Data.Data, nil
			},
			want:     pngBytes,
			wantBody: map[string]any{"url": "https://example.com", "sync": true, "screenshot_options": map[string]any{"full_page": false}},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s, c := newStub(t, tc.reply)
			got, err := tc.call(c)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("data = %#v\nwant   %#v", got, tc.want)
			}
			req := s.last(t)
			if req.Method != http.MethodPost || req.Path != tc.path {
				t.Errorf("request = %s %s, want POST %s", req.Method, req.Path, tc.path)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer test-key" {
				t.Errorf("Authorization = %q", got)
			}
			if got := req.Header.Get("User-Agent"); got != "urlpipe-go/0.1.0" {
				t.Errorf("User-Agent = %q", got)
			}
			if got := req.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q", got)
			}
			if !reflect.DeepEqual(req.Body, tc.wantBody) {
				t.Errorf("body = %#v\nwant   %#v", req.Body, tc.wantBody)
			}
		})
	}
}

func data[T any](r *Response[T], err error) (any, error) {
	if err != nil {
		return nil, err
	}
	if r.Status != StatusCompleted {
		return nil, errors.New("status " + string(r.Status))
	}
	return r.Data, nil
}

func TestLighthouseKeepsTheRawJSON(t *testing.T) {
	_, c := newStub(t, jsonOK(`{"url":"https://example.com","categories":{},"metrics":{},"somethingNew":42}`))
	r, err := c.Lighthouse(ctx, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Data.Raw) != `{"url":"https://example.com","categories":{},"metrics":{},"somethingNew":42}` {
		t.Errorf("Raw = %s", r.Data.Raw)
	}
}

func TestCommonOptionsAreSent(t *testing.T) {
	s, c := newStub(t, text(200, "ok"))
	_, err := c.Markdown(ctx, "https://example.com", &Options{
		MaxAge:      MaxAgeString("3 days"),
		Labels:      map[string]string{"client": "acme"},
		Residential: true,
		ReportTo:    "https://hooks.example.com/urlpipe",
		PageOptions: map[string]any{"block_ads": true, "remove_selectors": []string{".ad"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"url": "https://example.com", "sync": true, "max_age": "3 days",
		"labels": map[string]any{"client": "acme"}, "residential": true,
		"report_to":    "https://hooks.example.com/urlpipe",
		"page_options": map[string]any{"block_ads": true, "remove_selectors": []any{".ad"}},
	}
	if got := s.last(t).Body; !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v\nwant   %#v", got, want)
	}
}

func TestMaxAgeForms(t *testing.T) {
	for _, tc := range []struct {
		age  MaxAge
		want any
	}{
		{MaxAgeSeconds(0), 0.0},
		{MaxAgeSeconds(3600), 3600.0},
		{MaxAgeDuration(2 * time.Hour), 7200.0},
		{MaxAgeString("30m"), "30m"},
	} {
		s, c := newStub(t, text(200, "ok"))
		if _, err := c.HTML(ctx, "https://example.com", &Options{MaxAge: tc.age}); err != nil {
			t.Fatal(err)
		}
		if got := s.last(t).Body["max_age"]; got != tc.want {
			t.Errorf("max_age = %#v, want %#v", got, tc.want)
		}
	}
}

func TestExtraIsMergedAndIdempotencyKeyIsAHeader(t *testing.T) {
	s, c := newStub(t, text(200, "ok"))
	_, err := c.Markdown(ctx, "https://example.com", &Options{
		IdempotencyKey: "my-key-1",
		Extra:          map[string]any{"future_option": "yes", "page_options": map[string]any{"delay": 500}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := s.last(t)
	if got := req.Header.Get("Idempotency-Key"); got != "my-key-1" {
		t.Errorf("Idempotency-Key = %q", got)
	}
	if _, ok := req.Body["idempotency_key"]; ok {
		t.Error("the idempotency key leaked into the body")
	}
	if req.Body["future_option"] != "yes" {
		t.Errorf("extra param missing: %#v", req.Body)
	}
	if !reflect.DeepEqual(req.Body["page_options"], map[string]any{"delay": 500.0}) {
		t.Errorf("page_options = %#v", req.Body["page_options"])
	}
}

func TestHeadersBecomeMeta(t *testing.T) {
	_, c := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		standardHeaders(w.Header())
		w.Header().Set("X-Quota-Limit", "unlimited")
		w.Header().Set("X-Quota-Remaining", "unlimited")
		w.Header().Set("X-Concurrency-Limit", "unlimited")
		w.Header().Set("Idempotent-Replayed", "true")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("# Hi"))
	})
	r, err := c.Markdown(ctx, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Token != "tok_123" || !reflect.DeepEqual(r.Labels, map[string]string{"client": "acme"}) {
		t.Errorf("token %q labels %v", r.Token, r.Labels)
	}
	m := r.Meta
	if m.Cache != "hit" || *m.CacheAge != 5400 || *m.ProcessingTimeMs != 12 {
		t.Errorf("cache meta = %q %v %v", m.Cache, m.CacheAge, m.ProcessingTimeMs)
	}
	if *m.Quota.Cost != 0 || *m.Quota.Overage != 0 {
		t.Errorf("quota = %+v", m.Quota)
	}
	if !m.Quota.Limit.Unlimited || m.Quota.Limit.Raw != "unlimited" || !m.Quota.Remaining.Unlimited || !m.ConcurrencyLimit.Unlimited {
		t.Errorf("unlimited not read: %+v %+v %+v", m.Quota.Limit, m.Quota.Remaining, m.ConcurrencyLimit)
	}
	if want := time.Date(2026, 8, 31, 23, 59, 59, 0, time.UTC); !m.Quota.ResetsAt.Equal(want) {
		t.Errorf("resets_at = %v", m.Quota.ResetsAt)
	}
	if !m.IdempotentReplayed {
		t.Error("Idempotent-Replayed not read")
	}
}

func TestNumericQuotaAndMissingHeaders(t *testing.T) {
	_, c := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Quota-Limit", "1000")
		w.Header().Set("X-Quota-Remaining", "943")
		w.Header().Set("X-Cache", "miss")
		_, _ = w.Write([]byte("ok"))
	})
	r, err := c.Markdown(ctx, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := r.Meta
	if m.Quota.Limit.Value != 1000 || m.Quota.Limit.Unlimited || m.Quota.Remaining.Value != 943 {
		t.Errorf("quota = %+v %+v", m.Quota.Limit, m.Quota.Remaining)
	}
	if m.CacheAge != nil || m.ProcessingTimeMs != nil || m.Quota.Cost != nil || m.Quota.Overage != nil ||
		m.Quota.ResetsAt != nil || m.ConcurrencyLimit != nil || m.ResultURL != "" || m.IdempotentReplayed {
		t.Errorf("missing headers must stay empty: %+v", m)
	}
	if r.Token != "" || r.Labels == nil || len(r.Labels) != 0 {
		t.Errorf("token %q labels %v", r.Token, r.Labels)
	}
}

var (
	pngBytes  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	jpegBytes = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	webpBytes = []byte("RIFF\x1a\x00\x00\x00WEBPVP8 ")
)

func TestScreenshotDecodesAndDetectsTheFormat(t *testing.T) {
	for _, tc := range []struct {
		name  string
		image []byte
		mime  string
	}{
		{"png", pngBytes, "image/png"},
		{"jpeg", jpegBytes, "image/jpeg"},
		{"webp", webpBytes, "image/webp"},
		{"unknown falls back to png", []byte("not an image"), "image/png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			image := tc.image
			_, c := newStub(t, func(w http.ResponseWriter, r *http.Request) {
				standardHeaders(w.Header())
				w.Header().Set("X-Result-Url", "https://urlpipe.dev/r/abc.png")
				_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(image) + "\n"))
			})
			r, err := c.Screenshot(ctx, "https://example.com", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(r.Data.Data, image) || r.Data.MIMEType != tc.mime {
				t.Errorf("screenshot = %q %s", r.Data.Data, r.Data.MIMEType)
			}
			if r.Data.ResultURL != "https://urlpipe.dev/r/abc.png" || r.Meta.ResultURL != "https://urlpipe.dev/r/abc.png" {
				t.Errorf("result url = %q / %q", r.Data.ResultURL, r.Meta.ResultURL)
			}
		})
	}
}

func TestScreenshotSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.png")
	s := &Screenshot{Data: pngBytes}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(got, pngBytes) {
		t.Errorf("saved %q, %v", got, err)
	}
}

func TestAsyncReturnsTheAcceptedToken(t *testing.T) {
	s, c := newStub(t, jsonReply(200, `{"token":"tok_async","status":"accepted","labels":{"client":"acme"}}`,
		"X-Result-Token", "tok_async", "X-Cache", "miss", "X-Quota-Cost", "17"))
	r, err := c.Summarize(ctx, "https://example.com", &Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusAccepted || r.Completed() || r.Token != "tok_async" || r.Data != "" {
		t.Errorf("response = %+v", r)
	}
	if r.Labels["client"] != "acme" || *r.Meta.Quota.Cost != 17 {
		t.Errorf("labels %v meta %+v", r.Labels, r.Meta)
	}
	if got := s.last(t).Body["sync"]; got != false {
		t.Errorf("sync = %#v, want false", got)
	}
}

func TestAsyncScreenshotIsNotDecoded(t *testing.T) {
	_, c := newStub(t, jsonReply(200, `{"token":"tok_async","status":"accepted","labels":{}}`))
	r, err := c.Screenshot(ctx, "https://example.com", &ScreenshotOptions{Options: Options{Async: true}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusAccepted || r.Data != nil || r.Token != "tok_async" {
		t.Errorf("response = %+v", r)
	}
}

func TestSyncTimeoutPollsUntilTheResultIsReady(t *testing.T) {
	s, c := newStub(t,
		jsonReply(504, `{"error":"processing_timeout","message":"The analysis is taking longer than expected.","token":"tok_slow"}`),
		jsonReply(202, `{"status":"processing","token":"tok_slow","labels":{}}`),
		jsonReply(202, `{"status":"processing","token":"tok_slow","labels":{}}`),
		func(w http.ResponseWriter, r *http.Request) {
			standardHeaders(w.Header())
			w.Header().Set("X-Result-Token", "tok_slow")
			w.Header().Set("X-Result-Url", "https://urlpipe.dev/r/slow.png")
			_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(webpBytes)))
		},
	)
	r, err := c.Screenshot(ctx, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusCompleted || r.Data.MIMEType != "image/webp" || r.Token != "tok_slow" || r.Data.ResultURL != "https://urlpipe.dev/r/slow.png" {
		t.Errorf("response = %+v data %+v", r, r.Data)
	}
	reqs := s.requests()
	if len(reqs) != 4 {
		t.Fatalf("requests = %d, want 4", len(reqs))
	}
	for _, req := range reqs[1:] {
		if req.Method != http.MethodGet || req.Path != "/result/tok_slow" {
			t.Errorf("poll = %s %s", req.Method, req.Path)
		}
		if req.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("poll without Authorization")
		}
	}
}

func TestSyncTimeoutGivesUpWithWaitTimeout(t *testing.T) {
	_, c := newStub(t,
		jsonReply(504, `{"error":"processing_timeout","token":"tok_slow"}`),
		jsonReply(202, `{"status":"processing","token":"tok_slow"}`),
	)
	c.waitTimeout = 20 * time.Millisecond
	_, err := c.Lighthouse(ctx, "https://example.com", nil)
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("err = %v, want ErrWaitTimeout", err)
	}
	var e *Error
	if !errors.As(err, &e) || e.Token != "tok_slow" || e.Kind != KindWaitTimeout {
		t.Errorf("error = %+v", e)
	}
}

func TestSyncTimeoutPollingSurfacesTheRealError(t *testing.T) {
	_, c := newStub(t,
		jsonReply(504, `{"error":"processing_timeout","token":"tok_slow"}`),
		jsonReply(422, `{"error":"The request timed out."}`),
	)
	_, err := c.Markdown(ctx, "https://example.com", nil)
	var af *AnalysisFailedError
	if !errors.As(err, &af) || af.Message != "The request timed out." {
		t.Fatalf("err = %v", err)
	}
}

func TestResult(t *testing.T) {
	t.Run("completed", func(t *testing.T) {
		s, c := newStub(t, jsonOK(`["a","b"]`))
		r, err := c.Result(ctx, "tok_1", OperationKeywords)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != StatusCompleted || !reflect.DeepEqual(r.Data, []string{"a", "b"}) {
			t.Errorf("response = %+v", r)
		}
		if req := s.last(t); req.Method != http.MethodGet || req.Path != "/result/tok_1" || req.Header.Get("Idempotency-Key") != "" {
			t.Errorf("request = %+v", req)
		}
	})
	t.Run("processing is not an error", func(t *testing.T) {
		_, c := newStub(t, jsonReply(202, `{"status":"processing","token":"tok_1","labels":{"client":"acme"}}`))
		r, err := c.Result(ctx, "tok_1", "")
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != StatusProcessing || r.Token != "tok_1" || r.Labels["client"] != "acme" || r.Data != nil {
			t.Errorf("response = %+v", r)
		}
	})
	t.Run("without a hint JSON stays generic", func(t *testing.T) {
		_, c := newStub(t, jsonOK(`{"title":"Hi"}`))
		r, err := c.Result(ctx, "tok_1", "")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(r.Data, map[string]any{"title": "Hi"}) {
			t.Errorf("data = %#v", r.Data)
		}
	})
	t.Run("without a hint a screenshot stays Base64", func(t *testing.T) {
		b64 := base64.StdEncoding.EncodeToString(pngBytes)
		_, c := newStub(t, text(200, b64))
		r, err := c.Result(ctx, "tok_1", "")
		if err != nil {
			t.Fatal(err)
		}
		if r.Data != b64 {
			t.Errorf("data = %#v", r.Data)
		}
	})
}

func TestWaitWithTheScreenshotHintDecodes(t *testing.T) {
	_, c := newStub(t,
		jsonReply(202, `{"status":"processing","token":"tok_1"}`),
		text(200, base64.StdEncoding.EncodeToString(jpegBytes)),
	)
	r, err := c.Wait(ctx, "tok_1", OperationScreenshot, nil)
	if err != nil {
		t.Fatal(err)
	}
	shot, ok := r.Data.(*Screenshot)
	if !ok || shot.MIMEType != "image/jpeg" {
		t.Errorf("data = %#v", r.Data)
	}
}

func TestWaitTimesOut(t *testing.T) {
	_, c := newStub(t, jsonReply(202, `{"status":"processing","token":"tok_1"}`))
	_, err := c.Wait(ctx, "tok_1", OperationMarkdown, &WaitOptions{Timeout: 10 * time.Millisecond, Interval: time.Millisecond})
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindWaitTimeout || e.Token != "tok_1" {
		t.Fatalf("err = %v", err)
	}
}

func TestWaitStopsWithTheContext(t *testing.T) {
	_, c := newStub(t, jsonReply(202, `{"status":"processing","token":"tok_1"}`))
	cctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err := c.Wait(cctx, "tok_1", "", &WaitOptions{Timeout: time.Minute, Interval: 5 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestScrapeAccessors(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString(pngBytes)
	_, c := newStub(t, jsonOK(`{"url":"https://example.com","operations":{
		"markdown":{"success":true,"result":"# Hi","cached":false},
		"html":{"success":true,"result":"<p>Hi</p>","cached":true},
		"summarize":{"success":false,"result":null,"error":"The page is too big to be processed.","cached":false},
		"meta":{"success":true,"result":{"title":"Example Domain","language":"en"},"cached":true},
		"keywords":{"success":true,"result":["a","b"],"cached":false},
		"console":{"success":true,"result":[],"cached":false},
		"screenshot":{"success":true,"result":"`+b64+`","cached":false},
		"lighthouse":{"success":false,"error":"processing_timeout","cached":false}}}`))
	r, err := c.Scrape(ctx, "https://example.com", []Operation{OperationMarkdown}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Data
	if md, err := s.Markdown(); md != "# Hi" || err != nil {
		t.Errorf("markdown = %q %v", md, err)
	}
	if html, err := s.HTML(); html != "<p>Hi</p>" || err != nil || !s.Operations[OperationHTML].Cached {
		t.Errorf("html = %q %v", html, err)
	}
	var opErr *OperationError
	if _, err := s.Summary(); !errors.As(err, &opErr) || opErr.Message != "The page is too big to be processed." || opErr.Pending() {
		t.Errorf("summary err = %v", err)
	}
	if m, err := s.Meta(); err != nil || m.Title != "Example Domain" {
		t.Errorf("meta = %+v %v", m, err)
	}
	if k, err := s.Keywords(); err != nil || !reflect.DeepEqual(k, []string{"a", "b"}) {
		t.Errorf("keywords = %v %v", k, err)
	}
	if con, err := s.Console(); err != nil || con == nil || len(con) != 0 {
		t.Errorf("console = %#v %v", con, err)
	}
	if shot, err := s.Screenshot(); err != nil || !reflect.DeepEqual(shot.Data, pngBytes) || shot.MIMEType != "image/png" {
		t.Errorf("screenshot = %+v %v", shot, err)
	}
	if _, err := s.Lighthouse(); !errors.As(err, &opErr) || !opErr.Pending() {
		t.Errorf("lighthouse err = %v", err)
	}
	delete(s.Operations, OperationKeywords)
	if _, err := s.Keywords(); !errors.As(err, &opErr) || opErr.Message != "not requested" {
		t.Errorf("missing op err = %v", err)
	}
}

func TestContextCancellationIsNotAConnectionError(t *testing.T) {
	_, c := newStub(t, text(200, "ok"))
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err := c.Markdown(cctx, "https://example.com", nil)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrConnection) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheTimeoutBoundsEachRequest(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	_, c := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	c.timeout = 20 * time.Millisecond
	c.maxRetries = 0
	_, err := c.Markdown(ctx, "https://example.com", nil)
	if !errors.Is(err, ErrConnection) {
		t.Fatalf("err = %v, want ErrConnection", err)
	}
}

func TestNoLabelsIsAnEmptyMap(t *testing.T) {
	_, c := newStub(t, jsonReply(200, `{"token":"tok_async","status":"accepted"}`))
	r, err := c.Markdown(ctx, "https://example.com", &Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Labels == nil || len(r.Labels) != 0 {
		t.Errorf("labels = %#v, want an empty map", r.Labels)
	}
}

func TestExtraMayOverrideSync(t *testing.T) {
	s, c := newStub(t, jsonReply(200, `{"token":"tok_async","status":"accepted","labels":{}}`))
	r, err := c.Meta(ctx, "https://example.com", &Options{Extra: map[string]any{"sync": false}})
	if err != nil {
		t.Fatal(err)
	}
	if s.last(t).Body["sync"] != false || r.Status != StatusAccepted || r.Data != nil || r.Token != "tok_async" {
		t.Errorf("response = %+v, body %v", r, s.last(t).Body)
	}
}

func TestResultAnswering504ProcessingTimeoutIsProcessing(t *testing.T) {
	s, c := newStub(t, jsonReply(504, `{"error":"processing_timeout","token":"tok_1"}`))
	r, err := c.Result(ctx, "tok_1", OperationMarkdown)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusProcessing || r.Token != "tok_1" || len(s.requests()) != 1 {
		t.Errorf("response = %+v after %d requests", r, len(s.requests()))
	}
}

func TestResultAnsweringAnother504IsAServerError(t *testing.T) {
	s, c := newStub(t, jsonReply(504, `{"error":"gateway_timeout"}`))
	_, err := c.Result(ctx, "tok_1", "")
	if !errors.Is(err, ErrServer) {
		t.Fatalf("err = %v", err)
	}
	if n := len(s.requests()); n != 1 {
		t.Errorf("504 was retried: %d requests", n)
	}
}

func TestResultIsRetriedWithoutAnIdempotencyKey(t *testing.T) {
	s, c := newStub(t, jsonReply(503, ``), hangUp(t), text(200, "# Hi"))
	r, err := c.Result(ctx, "tok_1", OperationMarkdown)
	if err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if r.Data != "# Hi" || len(reqs) != 3 {
		t.Fatalf("data %#v after %d requests", r.Data, len(reqs))
	}
	for _, req := range reqs {
		if req.Header.Get("Idempotency-Key") != "" {
			t.Error("GET /result carried an Idempotency-Key")
		}
	}
}

func TestScrapeKeepsTheOperationOrder(t *testing.T) {
	_, c := newStub(t, jsonOK(`{"url":"https://example.com","operations":{
		"screenshot":{"success":true,"result":"","cached":false},
		"markdown":{"success":true,"result":"# Hi","cached":false},
		"meta":{"success":true,"result":{},"cached":false}}}`))
	r, err := c.Scrape(ctx, "https://example.com", []Operation{OperationScreenshot, OperationMarkdown, OperationMeta}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Operation{OperationScreenshot, OperationMarkdown, OperationMeta}; !reflect.DeepEqual(r.Data.Order, want) {
		t.Errorf("order = %v, want %v", r.Data.Order, want)
	}
}
