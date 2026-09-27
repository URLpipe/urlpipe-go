package urlpipe

import (
	"context"
)

func (c *Client) run(ctx context.Context, op Operation, url string, o *Options, specific map[string]any) (*Response[any], error) {
	return c.analyze(ctx, op, buildBody(url, o, specific), o)
}

// Markdown converts the page at url to Markdown (1 credit).
func (c *Client) Markdown(ctx context.Context, url string, opts *Options) (*Response[string], error) {
	r, err := c.run(ctx, OperationMarkdown, url, opts, nil)
	if err != nil {
		return nil, err
	}
	return typed[string](r), nil
}

// HTML returns the page's HTML after Chrome has rendered it (1 credit).
func (c *Client) HTML(ctx context.Context, url string, opts *Options) (*Response[string], error) {
	r, err := c.run(ctx, OperationHTML, url, opts, nil)
	if err != nil {
		return nil, err
	}
	return typed[string](r), nil
}

// Summarize returns a summary of the page, as Markdown (17 credits).
func (c *Client) Summarize(ctx context.Context, url string, opts *Options) (*Response[string], error) {
	r, err := c.run(ctx, OperationSummarize, url, opts, nil)
	if err != nil {
		return nil, err
	}
	return typed[string](r), nil
}

// Screenshot captures the page, full-page PNG unless opts.Screenshot says
// otherwise (1 credit). Data holds the decoded image.
func (c *Client) Screenshot(ctx context.Context, url string, opts *ScreenshotOptions) (*Response[*Screenshot], error) {
	var (
		o        *Options
		specific map[string]any
	)
	if opts != nil {
		o = &opts.Options
		if opts.Screenshot != nil {
			specific = map[string]any{"screenshot_options": opts.Screenshot}
		}
	}
	r, err := c.run(ctx, OperationScreenshot, url, o, specific)
	if err != nil {
		return nil, err
	}
	return typed[*Screenshot](r), nil
}

// Meta extracts the page's metadata: title, description, language, author,
// publication date, feed and images (5 credits).
func (c *Client) Meta(ctx context.Context, url string, opts *Options) (*Response[*Metadata], error) {
	r, err := c.run(ctx, OperationMeta, url, opts, nil)
	if err != nil {
		return nil, err
	}
	return typed[*Metadata](r), nil
}

// Keywords returns 5 to 15 keywords for the page, most relevant first
// (15 credits).
func (c *Client) Keywords(ctx context.Context, url string, opts *Options) (*Response[[]string], error) {
	r, err := c.run(ctx, OperationKeywords, url, opts, nil)
	if err != nil {
		return nil, err
	}
	return typed[[]string](r), nil
}

// Console returns the errors, warnings and uncaught exceptions the page
// logs while it loads (1 credit). An empty list means a clean page.
func (c *Client) Console(ctx context.Context, url string, opts *Options) (*Response[[]ConsoleEntry], error) {
	r, err := c.run(ctx, OperationConsole, url, opts, nil)
	if err != nil {
		return nil, err
	}
	return typed[[]ConsoleEntry](r), nil
}

// Lighthouse runs a Lighthouse audit of the page (2 credits). Audits can
// outlast the API's 60-second sync window; the client then polls for the
// result for you, up to the wait timeout.
func (c *Client) Lighthouse(ctx context.Context, url string, opts *LighthouseOptions) (*Response[*Lighthouse], error) {
	var o *Options
	specific := map[string]any{}
	if opts != nil {
		o = &opts.Options
		if opts.Device != "" {
			specific["device"] = opts.Device
		}
		if opts.IncludeAudits {
			specific["include_audits"] = true
		}
	}
	r, err := c.run(ctx, OperationLighthouse, url, o, specific)
	if err != nil {
		return nil, err
	}
	return typed[*Lighthouse](r), nil
}

// Scrape runs several operations off one visit to the page. Each operation
// is billed as usual; the page is loaded once, so the whole set arrives much
// sooner. Operations fail independently: read each one with the
// [ScrapeResult] accessors. When every operation fails, the error is an
// [*AnalysisFailedError] whose Scrape field holds each one's error.
func (c *Client) Scrape(ctx context.Context, url string, operations []Operation, opts *ScrapeOptions) (*Response[*ScrapeResult], error) {
	ops := make([]string, len(operations))
	for i, op := range operations {
		ops[i] = string(op)
	}
	var o *Options
	specific := map[string]any{"operations": ops}
	if opts != nil {
		o = &opts.Options
		if opts.Device != "" {
			specific["device"] = opts.Device
		}
		if opts.IncludeAudits {
			specific["include_audits"] = true
		}
		if opts.Screenshot != nil {
			specific["screenshot_options"] = opts.Screenshot
		}
	}
	r, err := c.run(ctx, OperationScrape, url, o, specific)
	if err != nil {
		return nil, err
	}
	return typed[*ScrapeResult](r), nil
}
