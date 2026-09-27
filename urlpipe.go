// Package urlpipe is the official Go client for the URLpipe API
// (https://urlpipe.dev), which turns a URL into clean data: Markdown, rendered
// HTML, a full-page screenshot, metadata, a summary, keywords, console errors,
// a Lighthouse audit, or several of these off one page visit.
//
// Create a client once and share it; it is safe for concurrent use:
//
//	client, err := urlpipe.NewClient() // reads URLPIPE_API_KEY
//	if err != nil {
//		log.Fatal(err)
//	}
//	res, err := client.Markdown(ctx, "https://example.com", nil)
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Println(res.Data)
//
// Every analysis method waits for the result by default (the API itself
// defaults to async). Set [Options.Async] to get a token back straight away
// and collect the result later with [Client.Wait] or a webhook.
//
// Requests that fail for a transient reason are retried, and every retried
// analysis carries the same Idempotency-Key, so a retry never runs or bills
// the work twice.
//
// Homepage: https://urlpipe.dev. Documentation: https://urlpipe.dev/docs.
// Source: https://github.com/URLpipe/urlpipe-go. Contact: contact@urlpipe.dev.
package urlpipe

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

// Version is the version of this library.
const Version = "0.1.0"

// DefaultBaseURL is where the URLpipe API lives.
const DefaultBaseURL = "https://urlpipe.dev"

// Defaults used by [NewClient] when the matching option is not given.
const (
	// DefaultTimeout bounds each HTTP request. The API holds a sync request
	// for up to 60 seconds, so this sits above that.
	DefaultTimeout = 90 * time.Second
	// DefaultMaxRetries is how many times a transient failure is retried.
	DefaultMaxRetries = 2
	// DefaultWaitTimeout bounds how long a long analysis is polled for.
	DefaultWaitTimeout = 300 * time.Second
	// DefaultPollInterval is the pause between two polls of GET /result/:token.
	DefaultPollInterval = 2 * time.Second
)

// APIKeyEnv is the environment variable read when no key is passed to
// [NewClient].
const APIKeyEnv = "URLPIPE_API_KEY"

const userAgent = "urlpipe-go/" + Version

// ErrMissingAPIKey is returned by [NewClient] when no API key was given and
// URLPIPE_API_KEY is empty.
var ErrMissingAPIKey = errors.New("urlpipe: an API key is required: pass urlpipe.WithAPIKey or set " + APIKeyEnv + " (find it in your project's settings at https://urlpipe.dev)")

// Client talks to the URLpipe API. Build one with [NewClient]; it is safe
// for concurrent use by multiple goroutines.
type Client struct {
	apiKey      string
	baseURL     string
	httpClient  *http.Client
	timeout     time.Duration
	maxRetries  int
	waitTimeout time.Duration

	// Tunables kept unexported so tests can run the retry and polling paths
	// without real sleeps.
	pollInterval time.Duration
	backoffBase  time.Duration
}

// Option configures a [Client].
type Option func(*Client)

// WithAPIKey sets the project API key. Without it, [NewClient] reads
// URLPIPE_API_KEY.
func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

// WithBaseURL points the client at another host, such as a local stub
// server in tests. The default is [DefaultBaseURL].
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

// WithHTTPClient sets the *http.Client requests are sent with, for custom
// transports, proxies or instrumentation. The per-request timeout set with
// [WithTimeout] still applies on top of it.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithTimeout bounds each HTTP request (default [DefaultTimeout]). Zero or
// less leaves requests bounded only by their context.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// WithMaxRetries sets how many times a transient failure is retried (default
// [DefaultMaxRetries]). Zero turns retries off, and with them the
// Idempotency-Key the client otherwise generates for every analysis.
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		if n < 0 {
			n = 0
		}
		c.maxRetries = n
	}
}

// WithWaitTimeout bounds how long a long analysis is polled for before
// giving up with [ErrWaitTimeout] (default [DefaultWaitTimeout]). It applies
// to the polling a sync call falls back to, and is the default for
// [Client.Wait].
func WithWaitTimeout(d time.Duration) Option {
	return func(c *Client) { c.waitTimeout = d }
}

// NewClient builds a client. It returns [ErrMissingAPIKey] when no API key
// was given with [WithAPIKey] and URLPIPE_API_KEY is empty.
func NewClient(opts ...Option) (*Client, error) {
	c := &Client{
		baseURL:      DefaultBaseURL,
		httpClient:   &http.Client{},
		timeout:      DefaultTimeout,
		maxRetries:   DefaultMaxRetries,
		waitTimeout:  DefaultWaitTimeout,
		pollInterval: DefaultPollInterval,
		backoffBase:  time.Second,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.apiKey == "" {
		c.apiKey = os.Getenv(APIKeyEnv)
	}
	if strings.TrimSpace(c.apiKey) == "" {
		return nil, ErrMissingAPIKey
	}
	return c, nil
}
