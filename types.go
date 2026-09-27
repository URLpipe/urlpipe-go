package urlpipe

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Metadata is the result of [Client.Meta]. A field the page does not carry
// is empty (nil for AdditionalAuthorInformation). URL fields are absolute.
type Metadata struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	// Language is an ISO 639-1 code such as "en".
	Language     string `json:"language"`
	MainImageURL string `json:"main_image_url"`
	FaviconURL   string `json:"favicon_url"`
	AuthorName   string `json:"author_name"`
	FeedURL      string `json:"feed_url"`
	// PublicationDate is the date of first publication, ISO 8601, as given.
	PublicationDate string `json:"publication_date"`
	// AdditionalAuthorInformation holds extra author details such as social
	// handles or an email address.
	AdditionalAuthorInformation map[string]any `json:"additional_author_information"`
}

// Console message types.
const (
	ConsoleError     = "error"
	ConsoleWarning   = "warning"
	ConsoleException = "exception"
)

// ConsoleEntry is one message from [Client.Console].
type ConsoleEntry struct {
	// Type is "error", "warning" or "exception".
	Type string `json:"type"`
	Text string `json:"text"`
}

// Screenshot is the decoded image from [Client.Screenshot].
type Screenshot struct {
	// Data is the image itself.
	Data []byte
	// MIMEType is image/png, image/jpeg or image/webp, read off the bytes.
	MIMEType string
	// ResultURL is a link to the image that needs no API key, valid for the
	// 30 days the result is kept. Empty when the response carried none.
	ResultURL string
}

// Save writes the image to path.
func (s *Screenshot) Save(path string) error {
	return os.WriteFile(path, s.Data, 0o644)
}

func decodeScreenshot(b64 string, resultURL string) (*Screenshot, error) {
	b64 = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', ' ', '\t':
			return -1
		}
		return r
	}, b64)
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("urlpipe: decoding the screenshot's Base64: %w", err)
		}
	}
	return &Screenshot{Data: data, MIMEType: detectMIME(data), ResultURL: resultURL}, nil
}

func detectMIME(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return "image/png"
}

// Lighthouse is the result of [Client.Lighthouse]. The documented fields are
// decoded; Raw holds the whole JSON object for anything else.
type Lighthouse struct {
	URL       string `json:"url"`
	FetchTime string `json:"fetchTime"`
	Device    string `json:"device"`
	// Categories has performance, accessibility, best-practices and seo.
	// A category Lighthouse no longer reports (pwa) is nil.
	Categories map[string]*LighthouseCategory `json:"categories"`
	// Metrics has first-contentful-paint, largest-contentful-paint,
	// cumulative-layout-shift, total-blocking-time and the rest. A metric
	// Lighthouse could not compute is nil.
	Metrics map[string]*LighthouseMetric `json:"metrics"`
	// Audits is only present with IncludeAudits.
	Audits map[string]json.RawMessage `json:"audits"`
	// Raw is the result exactly as the API sent it.
	Raw json.RawMessage `json:"-"`
}

// LighthouseCategory is one category score, from 0 to 1.
type LighthouseCategory struct {
	Score *float64 `json:"score"`
	Title string   `json:"title"`
}

// LighthouseMetric is one measured metric.
type LighthouseMetric struct {
	Score        *float64 `json:"score"`
	DisplayValue string   `json:"displayValue"`
	NumericValue *float64 `json:"numericValue"`
	NumericUnit  string   `json:"numericUnit"`
}

// UnmarshalJSON decodes the documented fields and keeps the raw object.
func (l *Lighthouse) UnmarshalJSON(b []byte) error {
	type plain Lighthouse
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*l = Lighthouse(p)
	l.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// ScrapeResult is the result of [Client.Scrape]: one entry per requested
// operation. Use the typed accessors (Markdown, Meta, Screenshot, ...) to
// read an operation's result.
type ScrapeResult struct {
	URL        string                        `json:"url"`
	Operations map[Operation]ScrapeOperation `json:"operations"`
	// Order lists the operations in the order the API returned them, which
	// is the order they were asked for.
	Order []Operation `json:"-"`
}

// UnmarshalJSON decodes the scrape object and records the operations' order.
func (s *ScrapeResult) UnmarshalJSON(b []byte) error {
	var aux struct {
		URL        string          `json:"url"`
		Operations json.RawMessage `json:"operations"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	*s = ScrapeResult{URL: aux.URL}
	if len(aux.Operations) == 0 || string(aux.Operations) == "null" {
		return nil
	}
	if err := json.Unmarshal(aux.Operations, &s.Operations); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(aux.Operations))
	if _, err := dec.Token(); err != nil { // {
		return err
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}
		if k, ok := key.(string); ok {
			s.Order = append(s.Order, Operation(k))
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return err
		}
	}
	return nil
}

// ScrapeOperation is one operation's outcome inside a scrape.
type ScrapeOperation struct {
	Success bool `json:"success"`
	// Result is the operation's result in its usual format, as JSON.
	Result json.RawMessage `json:"result"`
	// Error is the failure message when Success is false.
	Error string `json:"error"`
	// Cached is true when the operation was served from cache (free).
	Cached bool `json:"cached"`
}

// OperationError is returned by the [ScrapeResult] accessors when that
// operation failed, is still running, or was not requested.
type OperationError struct {
	Operation Operation
	Message   string
}

func (e *OperationError) Error() string {
	return fmt.Sprintf("urlpipe: scrape operation %s: %s", e.Operation, e.Message)
}

// Pending reports whether the operation (a lighthouse audit) was still
// running when the scrape answered. Fetch the scrape again with
// [Client.Result] and its token to pick it up.
func (e *OperationError) Pending() bool { return e.Message == "processing_timeout" }

func (s *ScrapeResult) result(op Operation) (any, error) {
	entry, ok := s.Operations[op]
	if !ok {
		return nil, &OperationError{Operation: op, Message: "not requested"}
	}
	if !entry.Success {
		msg := entry.Error
		if msg == "" {
			msg = "failed"
		}
		return nil, &OperationError{Operation: op, Message: msg}
	}
	return decodeJSONResult(op, entry.Result, "")
}

func scrapeGet[T any](s *ScrapeResult, op Operation) (T, error) {
	var zero T
	v, err := s.result(op)
	if err != nil {
		return zero, err
	}
	t, _ := v.(T)
	return t, nil
}

// Markdown is the markdown operation's result.
func (s *ScrapeResult) Markdown() (string, error) { return scrapeGet[string](s, OperationMarkdown) }

// HTML is the html operation's result.
func (s *ScrapeResult) HTML() (string, error) { return scrapeGet[string](s, OperationHTML) }

// Summary is the summarize operation's result.
func (s *ScrapeResult) Summary() (string, error) { return scrapeGet[string](s, OperationSummarize) }

// Screenshot is the screenshot operation's image, decoded.
func (s *ScrapeResult) Screenshot() (*Screenshot, error) {
	return scrapeGet[*Screenshot](s, OperationScreenshot)
}

// Meta is the meta operation's result.
func (s *ScrapeResult) Meta() (*Metadata, error) { return scrapeGet[*Metadata](s, OperationMeta) }

// Keywords is the keywords operation's result.
func (s *ScrapeResult) Keywords() ([]string, error) {
	return scrapeGet[[]string](s, OperationKeywords)
}

// Console is the console operation's result.
func (s *ScrapeResult) Console() ([]ConsoleEntry, error) {
	return scrapeGet[[]ConsoleEntry](s, OperationConsole)
}

// Lighthouse is the lighthouse operation's result.
func (s *ScrapeResult) Lighthouse() (*Lighthouse, error) {
	return scrapeGet[*Lighthouse](s, OperationLighthouse)
}

// isText reports whether op answers in text/plain.
func isText(op Operation) bool {
	switch op {
	case OperationMarkdown, OperationHTML, OperationSummarize, OperationScreenshot:
		return true
	}
	return false
}

// decodeBody turns a 200 body into the operation's typed result. With no
// operation hint, JSON parses as JSON and anything else stays a string.
func decodeBody(op Operation, body []byte, contentType, resultURL string) (any, error) {
	switch {
	case op == "":
		if strings.Contains(contentType, "json") {
			var v any
			if err := json.Unmarshal(body, &v); err != nil {
				return nil, fmt.Errorf("urlpipe: decoding the result: %w", err)
			}
			return v, nil
		}
		return string(body), nil
	case op == OperationScreenshot:
		return decodeScreenshot(string(body), resultURL)
	case isText(op):
		return string(body), nil
	}
	return decodeJSONResult(op, body, resultURL)
}

// decodeJSONResult types a result that arrives as a JSON value: a scrape
// entry, a webhook's result, or the body of a JSON operation.
func decodeJSONResult(op Operation, raw json.RawMessage, resultURL string) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if isText(op) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("urlpipe: decoding the %s result: %w", op, err)
		}
		if op == OperationScreenshot {
			return decodeScreenshot(s, resultURL)
		}
		return s, nil
	}
	var (
		v   any
		err error
	)
	switch op {
	case OperationMeta:
		v, err = decodeInto[*Metadata](raw)
	case OperationKeywords:
		v, err = decodeInto[[]string](raw)
	case OperationConsole:
		v, err = decodeInto[[]ConsoleEntry](raw)
	case OperationLighthouse:
		v, err = decodeInto[*Lighthouse](raw)
	case OperationScrape:
		v, err = decodeInto[*ScrapeResult](raw)
	default:
		v, err = decodeInto[any](raw)
	}
	if err != nil {
		return nil, fmt.Errorf("urlpipe: decoding the %s result: %w", op, err)
	}
	return v, nil
}

func decodeInto[T any](raw []byte) (T, error) {
	var v T
	err := json.Unmarshal(raw, &v)
	return v, err
}
