package urlpipe

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const maxRetryAfter = 60 * time.Second

// rawResponse is one HTTP answer, body read in full.
type rawResponse struct {
	status int
	header http.Header
	body   []byte
}

// send performs one request with retries. It returns the last answer the
// API gave (which may be an error status) or a transport error.
func (c *Client) send(ctx context.Context, method, path string, body []byte, idempotencyKey string) (*rawResponse, error) {
	for attempt := 0; ; attempt++ {
		res, err := c.sendOnce(ctx, method, path, body, idempotencyKey)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt >= c.maxRetries {
				return nil, connectionError(err)
			}
			if err := sleep(ctx, c.backoff(attempt)); err != nil {
				return nil, err
			}
			continue
		}
		wait, retry := c.retryDelay(res, attempt)
		if !retry || attempt >= c.maxRetries {
			return res, nil
		}
		if err := sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func (c *Client) sendOnce(ctx context.Context, method, path string, body []byte, idempotencyKey string) (*rawResponse, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &rawResponse{status: resp.StatusCode, header: resp.Header, body: data}, nil
}

// retryDelay decides whether an answer is worth another attempt, and after
// how long.
func (c *Client) retryDelay(res *rawResponse, attempt int) (time.Duration, bool) {
	switch res.status {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable:
		return c.backoff(attempt), true
	case http.StatusTooManyRequests:
		var obj map[string]any
		_ = json.Unmarshal(res.body, &obj)
		switch stringField(obj, "error") {
		case "concurrency_limit":
			return c.backoff(attempt), true
		case "rate_limited":
			d, ok := retryAfterGiven(res.header, obj)
			if !ok {
				d = c.backoffBase
			}
			if d > maxRetryAfter {
				d = maxRetryAfter
			}
			return d, true
		}
		// quota_exceeded, or a 429 the API does not document.
	}
	return 0, false
}

// backoff is 1s, 2s, 4s, ... for attempt 0, 1, 2, ..., capped at 60s.
func (c *Client) backoff(attempt int) time.Duration {
	if attempt > 30 {
		attempt = 30
	}
	d := c.backoffBase << uint(attempt)
	if d > maxRetryAfter || d <= 0 {
		return maxRetryAfter
	}
	return d
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// newIdempotencyKey is a random UUID v4.
func newIdempotencyKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// analyze runs one POST /<operation> and, when a sync call outlives the
// API's 60-second window, polls for the result.
func (c *Client) analyze(ctx context.Context, op Operation, body map[string]any, o *Options) (*Response[any], error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("urlpipe: encoding the request: %w", err)
	}
	key := ""
	if o != nil {
		key = o.IdempotencyKey
	}
	if key == "" && c.maxRetries > 0 {
		key = newIdempotencyKey()
	}
	res, err := c.send(ctx, http.MethodPost, "/"+string(op), payload, key)
	if err != nil {
		return nil, err
	}
	sync, _ := body["sync"].(bool)

	switch {
	case res.status == http.StatusOK && sync:
		return completed(op, res)
	case res.status == http.StatusOK:
		return pending(StatusAccepted, res)
	case res.status == http.StatusGatewayTimeout:
		var obj map[string]any
		_ = json.Unmarshal(res.body, &obj)
		token := stringField(obj, "token")
		if token == "" {
			token = res.header.Get("X-Result-Token")
		}
		if stringField(obj, "error") == "processing_timeout" && token != "" {
			return c.poll(ctx, token, op, c.waitTimeout, c.pollInterval)
		}
	}
	return nil, apiError(res.status, res.header, res.body)
}

func completed(op Operation, res *rawResponse) (*Response[any], error) {
	meta := parseMeta(res.header)
	data, err := decodeBody(op, res.body, res.header.Get("Content-Type"), meta.ResultURL)
	if err != nil {
		return nil, err
	}
	return &Response[any]{
		Status: StatusCompleted,
		Data:   data,
		Token:  res.header.Get("X-Result-Token"),
		Labels: parseLabels(res.header.Get("X-Labels")),
		Meta:   meta,
	}, nil
}

// pending builds the Response of an async accept or a 202 from GET /result.
func pending(status Status, res *rawResponse) (*Response[any], error) {
	var body struct {
		Token  string            `json:"token"`
		Status string            `json:"status"`
		Labels map[string]string `json:"labels"`
	}
	_ = json.Unmarshal(res.body, &body)
	r := &Response[any]{
		Status: status,
		Token:  res.header.Get("X-Result-Token"),
		Labels: parseLabels(res.header.Get("X-Labels")),
		Meta:   parseMeta(res.header),
	}
	if body.Status != "" {
		r.Status = Status(body.Status)
	}
	if body.Token != "" {
		r.Token = body.Token
	}
	if len(body.Labels) > 0 {
		r.Labels = body.Labels
	}
	return r, nil
}

// Result fetches the result behind a token once, with GET /result/:token.
// A finished analysis comes back with Status [StatusCompleted]; one still
// running comes back with [StatusProcessing] and no error.
//
// GET /result does not say which operation produced a token, so op types
// Data the way that operation's method would (a screenshot is decoded into
// a *Screenshot, meta into a *Metadata, ...). With op "", a JSON result is
// decoded into any and a text result stays a string, so a screenshot stays
// Base64.
func (c *Client) Result(ctx context.Context, token string, op Operation) (*Response[any], error) {
	res, err := c.send(ctx, http.MethodGet, "/result/"+url.PathEscape(token), nil, "")
	if err != nil {
		return nil, err
	}
	switch res.status {
	case http.StatusOK:
		r, err := completed(op, res)
		if err == nil && r.Token == "" {
			r.Token = token
		}
		return r, err
	case http.StatusGatewayTimeout:
		var obj map[string]any
		_ = json.Unmarshal(res.body, &obj)
		if stringField(obj, "error") != "processing_timeout" {
			break
		}
		fallthrough
	case http.StatusAccepted:
		r, err := pending(StatusProcessing, res)
		if err == nil && r.Token == "" {
			r.Token = token
		}
		return r, err
	}
	return nil, apiError(res.status, res.header, res.body)
}

// Wait polls GET /result/:token until the analysis finishes, fails, or the
// wait times out with [ErrWaitTimeout]. Use it for the token of an async
// call. op types Data as in [Client.Result]; pass OperationScreenshot to get
// a decoded *Screenshot. A nil opts waits for the client's wait timeout,
// polling every 2 seconds.
func (c *Client) Wait(ctx context.Context, token string, op Operation, opts *WaitOptions) (*Response[any], error) {
	timeout, interval := c.waitTimeout, c.pollInterval
	if opts != nil {
		if opts.Timeout > 0 {
			timeout = opts.Timeout
		}
		if opts.Interval > 0 {
			interval = opts.Interval
		}
	}
	return c.poll(ctx, token, op, timeout, interval)
}

func (c *Client) poll(ctx context.Context, token string, op Operation, timeout, interval time.Duration) (*Response[any], error) {
	deadline := time.Now().Add(timeout)
	for {
		r, err := c.Result(ctx, token, op)
		if err != nil {
			return nil, err
		}
		if r.Status == StatusCompleted {
			return r, nil
		}
		left := time.Until(deadline)
		if left <= 0 {
			return nil, waitTimeoutError(token, timeout)
		}
		if err := sleep(ctx, minDuration(interval, left)); err != nil {
			return nil, err
		}
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
