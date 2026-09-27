package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/URLpipe/urlpipe-go"
)

// printMeta writes what --verbose shows after a result: the token, whether it
// came from the cache, what it cost, the credits left and how long it took.
func printMeta(w io.Writer, token string, m urlpipe.Meta) {
	var parts []string
	if token != "" {
		parts = append(parts, "token "+token)
	}
	if m.Cache != "" {
		cache := "cache " + m.Cache
		if m.CacheAge != nil {
			cache += fmt.Sprintf(" (%s old)", time.Duration(*m.CacheAge)*time.Second)
		}
		parts = append(parts, cache)
	}
	if m.Quota.Cost != nil {
		parts = append(parts, fmt.Sprintf("cost %d %s", *m.Quota.Cost, plural(*m.Quota.Cost, "credit", "credits")))
	}
	if r := m.Quota.Remaining; r != nil {
		parts = append(parts, "remaining "+amount(r))
	}
	if m.ProcessingTimeMs != nil {
		parts = append(parts, fmt.Sprintf("took %s", time.Duration(*m.ProcessingTimeMs)*time.Millisecond))
	}
	if m.IdempotentReplayed {
		parts = append(parts, "replayed")
	}
	if len(parts) > 0 {
		fmt.Fprintln(w, strings.Join(parts, " · "))
	}
}

func amount(a *urlpipe.Amount) string {
	if a.Unlimited {
		return "unlimited"
	}
	if a.Raw != "" {
		return a.Raw
	}
	return fmt.Sprint(a.Value)
}

// apiFailure reports an error from the client on stderr, with a hint where
// one helps, and returns the exit code.
func (e *env) apiFailure(err error, op urlpipe.Operation, timeout time.Duration) int {
	if errors.Is(err, context.Canceled) {
		e.errorf("interrupted")
		return exitInterrupted
	}
	if errors.Is(err, context.DeadlineExceeded) {
		e.errorf("gave up after --timeout %s", timeout)
		return exitAPIError
	}

	var apiErr *urlpipe.Error
	if !errors.As(err, &apiErr) {
		e.errorf("%s", err)
		return exitAPIError
	}
	e.errorf("%s", apiErr.Message)

	var quota *urlpipe.QuotaExceededError
	var rate *urlpipe.RateLimitedError
	switch {
	case errors.Is(err, urlpipe.ErrAuthentication):
		fmt.Fprintln(e.stderr, "Run `urlpipe login`, or set "+urlpipe.APIKeyEnv+", with your project's API key.")
	case errors.As(err, &quota):
		if !quota.ResetsAt.IsZero() {
			fmt.Fprintf(e.stderr, "Your credits reset on %s; see https://urlpipe.dev/pricing for more.\n", quota.ResetsAt.Format("2 January 2006"))
		}
	case errors.As(err, &rate):
		if rate.RetryAfter > 0 {
			fmt.Fprintf(e.stderr, "Try again in %s.\n", rate.RetryAfter)
		}
	case errors.Is(err, urlpipe.ErrWaitTimeout) && apiErr.Token != "":
		hint := "urlpipe result " + apiErr.Token + " --wait"
		if op != "" {
			hint = "urlpipe result " + apiErr.Token + " --op " + string(op) + " --wait"
		}
		fmt.Fprintf(e.stderr, "The analysis is still running; collect it with `%s`.\n", hint)
	}
	return exitAPIError
}
