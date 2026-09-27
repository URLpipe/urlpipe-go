package urlpipe

import (
	"time"
)

// Operation names one of the API's analyses. It is what [Client.Scrape]
// takes a list of, and the hint [Client.Result] and [Client.Wait] use to
// type the data they return.
type Operation string

// The operations the API runs.
const (
	OperationMarkdown   Operation = "markdown"
	OperationHTML       Operation = "html"
	OperationSummarize  Operation = "summarize"
	OperationScreenshot Operation = "screenshot"
	OperationMeta       Operation = "meta"
	OperationKeywords   Operation = "keywords"
	OperationConsole    Operation = "console"
	OperationLighthouse Operation = "lighthouse"
	OperationScrape     Operation = "scrape"
)

// MaxAge is how fresh a cached result must be to be accepted. The zero
// value leaves it to the API (7 days). Build one with [MaxAgeSeconds],
// [MaxAgeDuration] or [MaxAgeString]; MaxAgeSeconds(0) always runs a fresh
// analysis.
type MaxAge struct {
	value any
}

// MaxAgeSeconds is a max_age of n seconds.
func MaxAgeSeconds(n int) MaxAge { return MaxAge{value: n} }

// MaxAgeDuration is a max_age of d, sent as whole seconds.
func MaxAgeDuration(d time.Duration) MaxAge { return MaxAge{value: int(d / time.Second)} }

// MaxAgeString is a max_age in the API's duration syntax, such as "3 days"
// or "2 hours". It is sent unchanged.
func MaxAgeString(s string) MaxAge { return MaxAge{value: s} }

// IsSet reports whether m carries a value.
func (m MaxAge) IsSet() bool { return m.value != nil }

// Options are the settings every analysis takes. The zero value, or a nil
// *Options, is a synchronous request with the API's defaults; only the
// fields you set are sent.
type Options struct {
	// Async returns as soon as the API accepts the request, with a token and
	// Status "accepted", instead of waiting for the result. The default
	// (false) sends sync: true and hands back the result.
	Async bool

	// MaxAge is how fresh a cached result must be. Cached results are free.
	MaxAge MaxAge

	// Labels are your own keys for the request, returned with the result.
	Labels map[string]string

	// Residential fetches the page from a home broadband address.
	Residential bool

	// ReportTo is a webhook URL the result is POSTed to (async requests).
	ReportTo string

	// PageOptions is sent as page_options unchanged: wait_for_selector,
	// delay, block_ads, block_cookie_banners, remove_selectors.
	PageOptions map[string]any

	// IdempotencyKey is sent as the Idempotency-Key header. Leave it empty
	// and the client generates one per call whenever retries are on.
	IdempotencyKey string

	// Extra is merged into the JSON body last, for API parameters this
	// version of the library has no field for.
	Extra map[string]any
}

// ScreenshotOptions are the settings of [Client.Screenshot].
type ScreenshotOptions struct {
	Options

	// Screenshot is sent as screenshot_options unchanged: full_page,
	// viewport_width, device_scale_factor, format, selector, dark_mode,
	// hide_selectors and the rest.
	Screenshot map[string]any
}

// LighthouseOptions are the settings of [Client.Lighthouse].
type LighthouseOptions struct {
	Options

	// Device is "mobile" (the API's default) or "desktop".
	Device string

	// IncludeAudits adds the full audits object (150+ audits).
	IncludeAudits bool
}

// ScrapeOptions are the settings of [Client.Scrape]. Device and
// IncludeAudits apply to a lighthouse operation, Screenshot to a screenshot
// operation.
type ScrapeOptions struct {
	Options

	Device        string
	IncludeAudits bool
	Screenshot    map[string]any
}

// WaitOptions tune [Client.Wait].
type WaitOptions struct {
	// Timeout bounds the wait. Zero uses the client's wait timeout.
	Timeout time.Duration
	// Interval is the pause between polls. Zero uses 2 seconds.
	Interval time.Duration
}

// buildBody assembles the JSON body of an analysis. Only the options that
// were set are sent; extra is merged last so it can carry anything.
func buildBody(url string, o *Options, specific map[string]any) map[string]any {
	if o == nil {
		o = &Options{}
	}
	body := map[string]any{"url": url, "sync": !o.Async}
	if o.MaxAge.IsSet() {
		body["max_age"] = o.MaxAge.value
	}
	if o.Labels != nil {
		body["labels"] = o.Labels
	}
	if o.Residential {
		body["residential"] = true
	}
	if o.ReportTo != "" {
		body["report_to"] = o.ReportTo
	}
	if o.PageOptions != nil {
		body["page_options"] = o.PageOptions
	}
	for k, v := range specific {
		body[k] = v
	}
	for k, v := range o.Extra {
		body[k] = v
	}
	return body
}
