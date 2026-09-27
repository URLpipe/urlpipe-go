# Changelog

## 0.2.0 - 2026-09-27

- `urlpipe`, a command-line tool in `cmd/urlpipe`: every operation as a command (`markdown`, `html`, `summarize`, `screenshot`, `meta`, `keywords`, `console`, `lighthouse`, `scrape`, `result`), `login` to save the API key, and exit codes for CI (`console --fail-on-errors`, `lighthouse --min-score`). Released as binaries for macOS, Linux and Windows, in the Homebrew tap `urlpipe/tap` and the Scoop bucket `URLpipe/scoop-bucket`.
- `WithUserAgent` names the program making the requests; the User-Agent then reads `<product> urlpipe-go/<version>`.

## 0.1.0 - 2026-09-25

The first release.

- `NewClient` with functional options; the API key falls back to `URLPIPE_API_KEY`.
- One method per operation: `Markdown`, `HTML`, `Summarize`, `Screenshot`, `Meta`, `Keywords`, `Console`, `Lighthouse` and `Scrape`, returning a typed `Response[T]`, plus `Result` and `Wait` for tokens.
- Sync by default; an analysis that outlasts the 60-second sync window is polled for until it finishes.
- Response headers parsed into `Meta`: cache, processing time, credits and limits.
- Typed errors that work with `errors.Is` and `errors.As`.
- Automatic retries of transient failures, each carrying one `Idempotency-Key` per call.
- `VerifyWebhook` for signed webhook deliveries.
