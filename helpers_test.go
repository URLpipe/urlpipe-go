package urlpipe

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// recorded is one request the stub server received.
type recorded struct {
	Method string
	Path   string
	Header http.Header
	Body   map[string]any
}

// stub is a local HTTP server answering with a scripted list of handlers,
// one per request; the last handler answers every request after it.
type stub struct {
	srv      *httptest.Server
	mu       sync.Mutex
	reqs     []recorded
	handlers []http.HandlerFunc
}

func newStub(t *testing.T, handlers ...http.HandlerFunc) (*stub, *Client) {
	t.Helper()
	s := &stub{handlers: handlers}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec := recorded{Method: r.Method, Path: r.URL.EscapedPath(), Header: r.Header.Clone()}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &rec.Body); err != nil {
				t.Errorf("request body is not a JSON object: %s", raw)
			}
		}
		s.mu.Lock()
		n := len(s.reqs)
		s.reqs = append(s.reqs, rec)
		s.mu.Unlock()
		if n >= len(s.handlers) {
			n = len(s.handlers) - 1
		}
		s.handlers[n](w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s, testClient(t, s.srv.URL)
}

func testClient(t *testing.T, baseURL string, opts ...Option) *Client {
	t.Helper()
	c, err := NewClient(append([]Option{WithAPIKey("test-key"), WithBaseURL(baseURL)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	c.backoffBase = time.Millisecond
	c.pollInterval = time.Millisecond
	return c
}

func (s *stub) requests() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recorded(nil), s.reqs...)
}

func (s *stub) last(t *testing.T) recorded {
	t.Helper()
	reqs := s.requests()
	if len(reqs) == 0 {
		t.Fatal("the stub received no request")
	}
	return reqs[len(reqs)-1]
}

// standardHeaders are the metadata headers of an ordinary finished request.
func standardHeaders(h http.Header) {
	h.Set("X-Result-Token", "tok_123")
	h.Set("X-Cache", "hit")
	h.Set("X-Cache-Age", "5400")
	h.Set("X-Processing-Time-Ms", "12")
	h.Set("X-Quota-Cost", "0")
	h.Set("X-Quota-Limit", "1000")
	h.Set("X-Quota-Remaining", "943")
	h.Set("X-Quota-Overage", "0")
	h.Set("X-Quota-Reset", "2026-08-31T23:59:59Z")
	h.Set("X-Concurrency-Limit", "3")
	h.Set("X-Labels", `{"client":"acme"}`)
}

func text(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		standardHeaders(w.Header())
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func jsonReply(status int, body string, headers ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i+1 < len(headers); i += 2 {
			w.Header().Set(headers[i], headers[i+1])
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func jsonOK(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		standardHeaders(w.Header())
		jsonReply(http.StatusOK, body)(w, r)
	}
}

// hangUp drops the connection without answering, like a network failure.
func hangUp(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("the test server cannot hijack connections")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	}
}
