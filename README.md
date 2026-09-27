# urlpipe-go

Turn any URL into clean data from Go: Markdown, rendered HTML, a full-page screenshot, metadata, a summary, keywords, console errors or a Lighthouse audit, each page rendered in real Chrome.

This is the official Go client for [URLpipe](https://urlpipe.dev). It uses the standard library only.

## Install

```sh
go get github.com/URLpipe/urlpipe-go
```

Requires Go 1.21 or later.

## Quickstart

Grab a project API key from your [dashboard](https://urlpipe.dev) and put it in `URLPIPE_API_KEY`. The Free plan gives you 1,000 credits a month, no card needed.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/URLpipe/urlpipe-go"
)

func main() {
	client, err := urlpipe.NewClient() // reads URLPIPE_API_KEY
	if err != nil {
		log.Fatal(err)
	}

	res, err := client.Markdown(context.Background(), "https://example.com", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Data)
}
```

Every call waits for the result and hands it back typed. The API itself defaults to async; the client sends `sync: true` for you, because a library caller almost always wants the answer. See [Async and Wait](#async-and-wait) to opt out.

## The client

```go
client, err := urlpipe.NewClient(
	urlpipe.WithAPIKey("..."),              // default: $URLPIPE_API_KEY
	urlpipe.WithBaseURL("https://urlpipe.dev"),
	urlpipe.WithTimeout(90*time.Second),    // per HTTP request
	urlpipe.WithMaxRetries(2),              // transient failures
	urlpipe.WithWaitTimeout(5*time.Minute), // how long a long analysis is polled for
	urlpipe.WithHTTPClient(&http.Client{}), // your own transport
)
```

`NewClient` returns `urlpipe.ErrMissingAPIKey` when there is no key. A `*Client` is safe to share between goroutines.

## Methods

Every method takes a `context.Context` first and the URL second, and returns a `*urlpipe.Response[T]`:

```go
type Response[T any] struct {
	Status Status            // "completed", "accepted" or "processing"
	Data   T                 // the typed result, set when Status is "completed"
	Token  string            // fetch the result again for free, for 30 days
	Labels map[string]string // the labels the request was made with
	Meta   Meta              // cache, credits, timing: see below
}
```

```go
ctx := context.Background()

md, _ := client.Markdown(ctx, url, nil)     // Data: string
html, _ := client.HTML(ctx, url, nil)       // Data: string, the HTML after JavaScript ran
sum, _ := client.Summarize(ctx, url, nil)   // Data: string, Markdown
meta, _ := client.Meta(ctx, url, nil)       // Data: *urlpipe.Metadata
kw, _ := client.Keywords(ctx, url, nil)     // Data: []string
logs, _ := client.Console(ctx, url, nil)    // Data: []urlpipe.ConsoleEntry

fmt.Println(meta.Data.Title, meta.Data.Language, meta.Data.PublicationDate)
for _, e := range logs.Data {
	fmt.Println(e.Type, e.Text) // "error", "warning" or "exception"
}
```

### Screenshot

The image arrives decoded, with its format read off the bytes and a link that needs no API key:

```go
shot, err := client.Screenshot(ctx, "https://example.com", &urlpipe.ScreenshotOptions{
	Screenshot: map[string]any{"format": "webp", "viewport_width": 390, "device_scale_factor": 2},
})
if err != nil {
	log.Fatal(err)
}
fmt.Println(shot.Data.MIMEType)  // image/webp
fmt.Println(shot.Data.ResultURL) // put it straight in an <img>
err = shot.Data.Save("example.webp")
```

### Lighthouse

```go
audit, err := client.Lighthouse(ctx, "https://example.com", &urlpipe.LighthouseOptions{
	Device:        "desktop", // default "mobile"
	IncludeAudits: true,      // add all 150+ audits
})
fmt.Println(*audit.Data.Categories["performance"].Score)
fmt.Println(audit.Data.Metrics["largest-contentful-paint"].DisplayValue)
// audit.Data.Raw holds the whole JSON for anything else.
```

### Scrape: several results off one page visit

```go
res, err := client.Scrape(ctx, "https://example.com",
	[]urlpipe.Operation{urlpipe.OperationMarkdown, urlpipe.OperationMeta, urlpipe.OperationScreenshot},
	nil)
if err != nil {
	log.Fatal(err)
}
md, err := res.Data.Markdown()
meta, err := res.Data.Meta()
shot, err := res.Data.Screenshot() // decoded, like Client.Screenshot
```

`res.Data.Order` lists the operations in the order you asked for them. Operations fail independently. An accessor returns a `*urlpipe.OperationError` for an operation that failed; its `Pending()` is true for a Lighthouse audit that was still running (fetch the scrape again with `client.Result(ctx, res.Token, urlpipe.OperationScrape)`). When every operation fails, `Scrape` returns an `*urlpipe.AnalysisFailedError` whose `Scrape` field holds each operation's error.

### Options

Every method takes an options struct; `nil` means the defaults, and only the fields you set are sent.

```go
res, err := client.Markdown(ctx, url, &urlpipe.Options{
	MaxAge:         urlpipe.MaxAgeString("3 days"), // or MaxAgeSeconds(3600), MaxAgeDuration(time.Hour)
	Labels:         map[string]string{"client": "acme"},
	Residential:    true,
	PageOptions:    map[string]any{"block_ads": true, "block_cookie_banners": true, "remove_selectors": []string{".newsletter"}},
	IdempotencyKey: "order-1234",
	Extra:          map[string]any{"some_new_param": true}, // merged into the body as-is
})
```

`ScreenshotOptions`, `LighthouseOptions` and `ScrapeOptions` embed `Options` and add their own fields. Cached results are free: `MaxAgeSeconds(0)` forces a fresh analysis, and a wider `MaxAge` lowers your bill.

### Meta

`res.Meta` is what the response headers said. A header the API did not send is `nil` (or `""`), never a panic.

```go
m := res.Meta
m.Cache              // "hit", "miss" or "partial"
m.CacheAge           // *int seconds, on a hit
m.ProcessingTimeMs   // *int
m.Quota.Cost         // *int credits this call spent
m.Quota.Remaining    // *Amount: .Value, or .Unlimited on an unlimited plan
m.Quota.Overage      // *int
m.Quota.ResetsAt     // *time.Time
m.ConcurrencyLimit   // *Amount: size your worker pool from it
m.IdempotentReplayed // true when an Idempotency-Key replayed an earlier answer
```

## Async and Wait

Set `Async: true` to get a token back at once and collect the result later, from a webhook or with `Wait`:

```go
accepted, err := client.Lighthouse(ctx, url, &urlpipe.LighthouseOptions{
	Options: urlpipe.Options{Async: true, ReportTo: "https://example.com/webhooks/urlpipe"},
})
// accepted.Status == urlpipe.StatusAccepted, accepted.Token is set

res, err := client.Wait(ctx, accepted.Token, urlpipe.OperationLighthouse, &urlpipe.WaitOptions{
	Timeout: 5 * time.Minute, Interval: 2 * time.Second,
})
audit := res.Data.(*urlpipe.Lighthouse)
```

`GET /result/:token` does not say which operation made a token, so `Result` and `Wait` take the operation as a hint and type `Data` as that operation's method would. Pass `""` and a JSON result decodes into `any` while a text result stays a `string`, so a screenshot stays Base64: use `client.Wait(ctx, token, urlpipe.OperationScreenshot, nil)` to get a decoded `*Screenshot`.

`client.Result(ctx, token, op)` is a single fetch: a finished result comes back with `StatusCompleted`, one still running with `StatusProcessing` and no error.

You rarely need `Wait` for sync calls. The API holds a sync request for 60 seconds; when an analysis takes longer, the client polls for the result every 2 seconds for up to the wait timeout, so you still see one call that returned the result. If the wait runs out, the error matches `urlpipe.ErrWaitTimeout` and carries the token to collect it later.

## Webhooks

`VerifyWebhook` checks a signed delivery and returns its payload. It needs no client:

```go
http.HandleFunc("/webhooks/urlpipe", func(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "unreadable body", http.StatusBadRequest)
		return
	}
	event, err := urlpipe.VerifyWebhook(body, r.Header, os.Getenv("URLPIPE_WEBHOOK_SECRET"), 5*time.Minute)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	data, _ := event.Data() // typed by event.Operation: string, *Screenshot, *Metadata, ...
	log.Println(event.Token, event.Success, data)
	w.WriteHeader(http.StatusNoContent)
})
```

Give it the **raw request body**, the exact bytes received. Decoding the JSON and encoding it again changes the bytes, and the signature no longer matches. It accepts a delivery when any `v1=` signature matches (so a secret rotation verifies with either secret), ignores other schemes, and refuses a timestamp further than the tolerance from now (zero means 5 minutes). A refusal is a `*urlpipe.WebhookVerificationError` with a `Reason`. Holding the two header values instead of an `http.Header`? Use `VerifyWebhookSignature(body, timestamp, signature, secret, tolerance)`.

## Errors

Every API and transport error is an `*urlpipe.Error` with `Kind`, `Status`, `Code`, `Message`, `Body` and `Token`. Test for a kind with `errors.Is`, and reach the extra fields with `errors.As`:

```go
_, err := client.Summarize(ctx, url, nil)

var quota *urlpipe.QuotaExceededError
var apiErr *urlpipe.Error
switch {
case errors.As(err, &quota):
	fmt.Printf("needs %d credits; the allowance resets %s\n", quota.Needed, quota.ResetsAt)
case errors.Is(err, urlpipe.ErrAnalysisFailed):
	fmt.Println("the page could not be analysed:", err)
case errors.As(err, &apiErr):
	fmt.Println(apiErr.Kind, apiErr.Status, apiErr.Code, apiErr.Message)
}
```

| Sentinel | When | Detailed type |
|---|---|---|
| `ErrAuthentication` | 401: the key is missing or not active | |
| `ErrEmailUnverified` | 403: confirm the email on the account | |
| `ErrInvalidRequest` | 422 with a code: `invalid_url`, `invalid_options`, ... | |
| `ErrAnalysisFailed` | 422: the page could not be analysed; the message says why | `*AnalysisFailedError` (`Scrape`) |
| `ErrQuotaExceeded` | 429: the Free plan's credits are spent | `*QuotaExceededError` (`Limit`, `Used`, `Needed`, `ResetsAt`) |
| `ErrConcurrencyLimit` | 429: every parallel request is running | `*ConcurrencyLimitError` (`Limit`, `Running`) |
| `ErrRateLimited` | 429: sending too fast | `*RateLimitedError` (`RetryAfter`) |
| `ErrNotFound` | 404: no result for this token | |
| `ErrStaleResult` | 410: the result is past its 30 days | |
| `ErrServer` | 5xx | |
| `ErrConnection` | the API could not be reached (`Unwrap` gives the cause) | |
| `ErrWaitTimeout` | the analysis was still running when the wait ended (`Token`) | |

Any other answer, such as a 403 other than `email_unverified` or a 429 with an unknown code, is a plain `*urlpipe.Error` with `Kind` `urlpipe.KindUnexpected`. `Code` is set only when the body's `error` is a code like `invalid_url`; when it is a sentence, the sentence is in `Message`. When every operation of a scrape fails, `Message` reads `Every operation failed: <op>: <error>; ...`.

`errors.As(err, &apiErr)` also works on the detailed types. A cancelled context comes back as the context's own error.

## Retries and idempotency

Connection errors, 500/502/503 and `concurrency_limit` are retried after 1 s, 2 s, 4 s, ... (at most 60 s); `rate_limited` after the `Retry-After` it names (else 1 s, at most 60 s). That happens up to `WithMaxRetries` times (default 2). A 401, 403, 404, 410, 422, 504, `quota_exceeded` or any other 429 is never retried.

A retry is only safe if it cannot run the work twice, so every analysis the client might retry carries an `Idempotency-Key`: yours from `Options.IdempotencyKey`, or a UUID the client generates for that call and reuses for each of its retries. The API answers a repeated key with the first request's token and result, charged once and delivered to your webhook once. `WithMaxRetries(0)` turns retries off, and the generated key with them.

## Links

- Homepage: https://urlpipe.dev
- Documentation: https://urlpipe.dev/docs
- Source: https://github.com/URLpipe/urlpipe-go
- API reference for this package: https://pkg.go.dev/github.com/URLpipe/urlpipe-go
- Pricing: https://urlpipe.dev/pricing
- MCP server, for using URLpipe from AI agents: https://github.com/URLpipe/mcp
- Questions: contact@urlpipe.dev

## License

MIT, see [LICENSE](LICENSE).
