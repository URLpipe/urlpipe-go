package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/URLpipe/urlpipe-go"
)

// newFlagSet builds a flag set that reports nothing on its own: run prints
// errors and help itself, so they go to the right stream.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// parseArgs parses flags wherever they sit, before or after the positional
// arguments (the flag package alone stops at the first non-flag). Anything
// after "--" is positional.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		consumed := len(args) - len(rest)
		if consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), nil
		}
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// setFlags lists the flags given on the command line.
func setFlags(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

// labelsValue collects repeated --label key=value flags.
type labelsValue map[string]string

func (l labelsValue) String() string {
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + l[k]
	}
	return strings.Join(parts, ",")
}

func (l labelsValue) Set(s string) error {
	k, v, ok := strings.Cut(s, "=")
	if !ok || strings.TrimSpace(k) == "" {
		return errors.New("want key=value")
	}
	l[strings.TrimSpace(k)] = v
	return nil
}

// durationValue is a Go duration ("90s", "5m") or a number of seconds.
type durationValue struct{ d time.Duration }

func (d *durationValue) String() string {
	if d == nil || d.d == 0 {
		return ""
	}
	return d.d.String()
}

func (d *durationValue) Set(s string) error {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		d.d = time.Duration(n) * time.Second
	} else if v, err := time.ParseDuration(s); err == nil {
		d.d = v
	} else {
		return errors.New("want a duration such as 90s or 5m, or a number of seconds")
	}
	if d.d <= 0 {
		return errors.New("must be more than zero")
	}
	return nil
}

// connFlags are the flags every command that calls the API takes.
type connFlags struct {
	apiKey  string
	baseURL string
	timeout durationValue
	verbose bool
}

func (c *connFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&c.apiKey, "api-key", "", "")
	fs.StringVar(&c.baseURL, "base-url", "", "") // hidden: for tests and local servers
	fs.Var(&c.timeout, "timeout", "")
	fs.BoolVar(&c.verbose, "verbose", false, "")
	fs.BoolVar(&c.verbose, "v", false, "")
}

// analysisFlags are the flags every analysis command takes.
type analysisFlags struct {
	connFlags
	maxAge             string
	labels             labelsValue
	residential        bool
	async              bool
	blockAds           bool
	blockCookieBanners bool
	waitFor            string
}

func (a *analysisFlags) register(fs *flag.FlagSet) {
	a.connFlags.register(fs)
	a.labels = labelsValue{}
	fs.StringVar(&a.maxAge, "max-age", "", "")
	fs.Var(a.labels, "label", "")
	fs.BoolVar(&a.residential, "residential", false, "")
	fs.BoolVar(&a.async, "async", false, "")
	fs.BoolVar(&a.blockAds, "block-ads", false, "")
	fs.BoolVar(&a.blockCookieBanners, "block-cookie-banners", false, "")
	fs.StringVar(&a.waitFor, "wait-for", "", "")
}

// usesPageOptions reports whether any flag that shapes the page was given.
func (a *analysisFlags) usesPageOptions() bool {
	return a.blockAds || a.blockCookieBanners || a.waitFor != ""
}

// options turns the flags into the client's Options.
func (a *analysisFlags) options() (urlpipe.Options, error) {
	o := urlpipe.Options{Async: a.async, Residential: a.residential}
	if a.maxAge != "" {
		m, err := parseMaxAge(a.maxAge)
		if err != nil {
			return o, err
		}
		o.MaxAge = m
	}
	if len(a.labels) > 0 {
		o.Labels = map[string]string(a.labels)
	}
	page := map[string]any{}
	if a.blockAds {
		page["block_ads"] = true
	}
	if a.blockCookieBanners {
		page["block_cookie_banners"] = true
	}
	if a.waitFor != "" {
		page["wait_for_selector"] = a.waitFor
	}
	if len(page) > 0 {
		o.PageOptions = page
	}
	return o, nil
}

// parseMaxAge reads a number of seconds, or passes a duration such as "3d",
// "2h" or "3 days" to the API as it is: the API knows its own units.
func parseMaxAge(s string) (urlpipe.MaxAge, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return urlpipe.MaxAge{}, errors.New("--max-age is empty")
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 {
			return urlpipe.MaxAge{}, errors.New("--max-age cannot be negative")
		}
		return urlpipe.MaxAgeSeconds(n), nil
	}
	return urlpipe.MaxAgeString(s), nil
}

// normalizeURL adds https:// to a bare host such as example.com.
func normalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if !strings.Contains(u, "://") {
		return "https://" + u
	}
	return u
}

// apiKey picks the key: the flag, then URLPIPE_API_KEY, then the saved
// config. source says where it came from.
func (c *connFlags) resolveKey() (key, source string, err error) {
	if k := strings.TrimSpace(c.apiKey); k != "" {
		return k, "--api-key", nil
	}
	if k := strings.TrimSpace(os.Getenv(urlpipe.APIKeyEnv)); k != "" {
		return k, urlpipe.APIKeyEnv, nil
	}
	cfg, path, err := loadConfig()
	if err != nil {
		return "", "", err
	}
	if k := strings.TrimSpace(cfg.APIKey); k != "" {
		return k, path, nil
	}
	return "", "", nil
}

// errNoKey is reported when no key was found anywhere.
var errNoKey = errors.New("no API key: run `urlpipe login`, set " + urlpipe.APIKeyEnv + " or pass --api-key (find the key in your project's settings at https://urlpipe.dev)")

// client builds the API client for a command.
func (c *connFlags) client() (*urlpipe.Client, error) {
	key, _, err := c.resolveKey()
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, errNoKey
	}
	opts := []urlpipe.Option{
		urlpipe.WithAPIKey(key),
		urlpipe.WithUserAgent("urlpipe-cli/" + cliVersion()),
	}
	if c.baseURL != "" {
		opts = append(opts, urlpipe.WithBaseURL(c.baseURL))
	}
	return urlpipe.NewClient(opts...)
}

// context bounds the whole command by --timeout, when given.
func (c *connFlags) context(parent context.Context) (context.Context, context.CancelFunc) {
	if c.timeout.d > 0 {
		return context.WithTimeout(parent, c.timeout.d)
	}
	return context.WithCancel(parent)
}

// parseCommand parses a command's flags and checks it got exactly one
// positional argument. ok is false when the command should stop with code.
func (e *env) parseCommand(cmd *command, fs *flag.FlagSet, args []string, what string) (arg string, code int, ok bool) {
	positional, err := parseArgs(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(e.stdout, commandHelp(cmd))
		return "", exitOK, false
	}
	if err != nil {
		return "", e.usageError(cmd.name, "%s", err), false
	}
	if what == "" {
		if len(positional) > 0 {
			return "", e.usageError(cmd.name, "%s takes no arguments; got %q", cmd.name, positional[0]), false
		}
		return "", 0, true
	}
	switch len(positional) {
	case 0:
		return "", e.usageError(cmd.name, "%s needs a %s", cmd.name, what), false
	case 1:
		return positional[0], 0, true
	}
	return "", e.usageError(cmd.name, "%s takes one %s; got %d arguments: %s", cmd.name, what, len(positional), strings.Join(positional, " ")), false
}
