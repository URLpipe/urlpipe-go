package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/URLpipe/urlpipe-go"
)

// command is one `urlpipe <name>` subcommand.
type command struct {
	name     string
	synopsis string // the usage line after "urlpipe "
	summary  string // one line for the command list
	about    string // the paragraph at the top of the command's help
	flags    string // the command's own flags, for its help
	kind     flagKind
	run      func(e *env, args []string) int
}

// flagKind says which shared flags a command takes, for its help.
type flagKind int

const (
	noSharedFlags flagKind = iota
	connectionFlags
	allAnalysisFlags
)

var commands []*command

func init() {
	commands = []*command{
		{
			name: "markdown", synopsis: "markdown <url> [flags]", kind: allAnalysisFlags,
			summary: "The page as Markdown, ready for a model or a note (1 credit)",
			about:   "Writes the page as Markdown to stdout. Costs 1 credit; a cached result is free.",
			run: func(e *env, args []string) int {
				return textCommand(e, lookup("markdown"), args, (*urlpipe.Client).Markdown)
			},
		},
		{
			name: "html", synopsis: "html <url> [flags]", kind: allAnalysisFlags,
			summary: "The page's HTML after JavaScript ran (1 credit)",
			about:   "Writes the page's HTML, as Chrome rendered it, to stdout. Costs 1 credit; a cached result is free.",
			run: func(e *env, args []string) int {
				return textCommand(e, lookup("html"), args, (*urlpipe.Client).HTML)
			},
		},
		{
			name: "summarize", synopsis: "summarize <url> [flags]", kind: allAnalysisFlags,
			summary: "A summary of the page, as Markdown (17 credits)",
			about:   "Writes a summary of the page, as Markdown, to stdout. Costs 17 credits; a cached result is free.",
			run: func(e *env, args []string) int {
				return textCommand(e, lookup("summarize"), args, (*urlpipe.Client).Summarize)
			},
		},
		{
			name: "screenshot", synopsis: "screenshot <url> [-o file] [flags]", kind: allAnalysisFlags,
			summary: "A full-page screenshot, saved to a file (1 credit)",
			about: "Saves a full-page screenshot. Without -o the file is named after the host and the\n" +
				"image format, such as example.com.png; -o - writes the image to stdout. The path\n" +
				"written, then the image's link (it needs no API key), go to stderr.\n" +
				"Costs 1 credit whatever the options; a cached result is free.",
			flags: `  -o, --output FILE       Where to write the image; - for stdout
      --format FORMAT     png (default), jpeg or webp
      --quality N         JPEG and WebP quality, 1-100 (default 80)
      --width N           Viewport width, 320-1920 (default 1350)
      --height N          Viewport height, 240-1080 (default 797)
      --scale N           Pixel density, 1-3; 2 is a retina image
      --viewport-only     Capture what fits in the viewport, not the whole page
      --selector CSS      Capture this one element instead of the page
      --dark              Render with prefers-color-scheme: dark
`,
			run: cmdScreenshot,
		},
		{
			name: "meta", synopsis: "meta <url> [flags]", kind: allAnalysisFlags,
			summary: "Title, description, language, author and images, as JSON (5 credits)",
			about: "Writes the page's metadata as indented JSON: title, description, language,\n" +
				"main_image_url, favicon_url, author_name, feed_url, publication_date and\n" +
				"additional_author_information. Costs 5 credits; a cached result is free.",
			run: cmdMeta,
		},
		{
			name: "keywords", synopsis: "keywords <url> [--json] [flags]", kind: allAnalysisFlags,
			summary: "5 to 15 keywords, most relevant first (15 credits)",
			about:   "Writes the page's keywords, most relevant first, one per line. Costs 15 credits;\na cached result is free.",
			flags:   "      --json              Write a JSON array instead\n",
			run:     cmdKeywords,
		},
		{
			name: "console", synopsis: "console <url> [--fail-on-errors] [--json] [flags]", kind: allAnalysisFlags,
			summary: "The errors, warnings and exceptions the page logs (1 credit)",
			about: "Writes what the page logs to the JavaScript console while it loads, one entry per\n" +
				"line as \"<type>: <text>\", where type is error, warning or exception. A line break\n" +
				"inside a message is written as \\n, so each entry stays on one line. A clean page\n" +
				"writes nothing. Console errors are data, so the exit code is 0 unless you ask for\n" +
				"--fail-on-errors. Costs 1 credit; a cached result is free.",
			flags: `      --fail-on-errors    Exit 3 when the page logged an error or an exception
      --json              Write a JSON array of {"type", "text"} instead
`,
			run: cmdConsole,
		},
		{
			name: "lighthouse", synopsis: "lighthouse <url> [--device mobile|desktop] [--min-score N] [--json] [flags]", kind: allAnalysisFlags,
			summary: "A Lighthouse audit: category scores and metrics (2 credits)",
			about: "Runs a Lighthouse audit and writes a summary: one line per category score (0-100),\n" +
				"then a blank line, then one line per metric, each a key and a value:\n\n" +
				"  performance               95\n" +
				"  largest-contentful-paint  2.5 s\n\n" +
				"A value Lighthouse could not compute is \"-\". An audit audits the page exactly as\n" +
				"it loads, so --block-ads, --block-cookie-banners and --wait-for do not apply.\n" +
				"Costs 2 credits; a cached result is free.",
			flags: `      --device DEVICE     mobile (default) or desktop
      --min-score N       Exit 3 when the performance score is below N (0-100)
      --include-audits    Add all 150+ audits to the --json output
      --json              Write the full audit as JSON instead
`,
			run: cmdLighthouse,
		},
		{
			name: "scrape", synopsis: "scrape <url> --ops markdown,meta,... [flags]", kind: allAnalysisFlags,
			summary: "Several results off one page visit, as JSON",
			about: "Runs several operations off one visit to the page, which gets you all of them\n" +
				"much sooner, and writes the combined result as indented JSON:\n" +
				"{\"url\", \"operations\": {\"<op>\": {\"success\", \"result\", \"error\", \"cached\"}}}.\n" +
				"Operations fail independently: a failed one is reported on stderr and the exit\n" +
				"code stays 0 while at least one succeeded. Each operation is billed as usual.",
			flags: `      --ops LIST          Comma-separated: markdown, html, summarize, screenshot,
                          meta, keywords, console, lighthouse (required)
      --device DEVICE     The lighthouse operation's device: mobile or desktop
      --include-audits    Add all 150+ audits to the lighthouse operation
`,
			run: cmdScrape,
		},
		{
			name: "result", synopsis: "result <token> [--op OPERATION] [--wait] [flags]", kind: connectionFlags,
			summary: "Fetch a stored result by its token (free)",
			about: "Fetches the result behind a token, free, for the 30 days it is kept. --op names\n" +
				"the operation that made the token, so the result is written the way that\n" +
				"command writes it; without --op, text is written as it is and JSON indented\n" +
				"(a screenshot then stays Base64). When the analysis is still running, result\n" +
				"exits 4; add --wait to wait for it instead.",
			flags: `      --op OPERATION      markdown, html, summarize, screenshot, meta, keywords,
                          console, lighthouse or scrape
      --wait              Wait until the analysis finishes (see --timeout)
      --interval D        How often to check while waiting (default 2s)
  -o, --output FILE       With --op screenshot: where to write the image
      --json              With --op keywords, console or lighthouse: write JSON
`,
			run: cmdResult,
		},
		{
			name: "login", synopsis: "login [--api-key KEY]", kind: noSharedFlags,
			summary: "Save your API key, so you don't pass it every time",
			about: "Asks for your project's API key and saves it to your user config directory\n" +
				"(" + describeConfigPath() + "), readable only by you.\n" +
				"Find the key in your project's settings at https://urlpipe.dev. The key is also\n" +
				"read from stdin when it is piped: echo \"$KEY\" | urlpipe login.",
			flags: "      --api-key KEY       Save this key without asking\n",
			run:   cmdLogin,
		},
		{
			name: "logout", synopsis: "logout", kind: noSharedFlags,
			summary: "Remove the saved API key",
			about:   "Removes the API key `urlpipe login` saved.",
			run:     cmdLogout,
		},
		{
			name: "version", synopsis: "version", kind: noSharedFlags,
			summary: "Print the version",
			about:   "Prints \"urlpipe <version>\".",
			run:     cmdVersion,
		},
		{
			name: "help", synopsis: "help [command]", kind: noSharedFlags,
			summary: "Help for a command",
			about:   "Prints the help for a command, or the list of commands.",
			run:     cmdHelp,
		},
	}
}

func lookup(name string) *command {
	for _, c := range commands {
		if c.name == name {
			return c
		}
	}
	return nil
}

// invocation is an analysis command, parsed and ready to call the API.
type invocation struct {
	e      *env
	cmd    *command
	op     urlpipe.Operation
	flags  *analysisFlags
	url    string
	opts   urlpipe.Options
	client *urlpipe.Client
	ctx    context.Context
	cancel context.CancelFunc
}

// prepare parses an analysis command. extra registers the command's own
// flags. ok is false when the command should stop with code.
func (e *env) prepare(cmd *command, args []string, extra func(fs *flag.FlagSet)) (inv *invocation, code int, ok bool) {
	fs := newFlagSet(cmd.name)
	a := &analysisFlags{}
	a.register(fs)
	if extra != nil {
		extra(fs)
	}
	target, code, ok := e.parseCommand(cmd, fs, args, "URL")
	if !ok {
		return nil, code, false
	}
	opts, err := a.options()
	if err != nil {
		return nil, e.usageError(cmd.name, "%s", err), false
	}
	client, err := a.client()
	if err != nil {
		return nil, e.configError(err), false
	}
	ctx, cancel := a.context(e.ctx)
	return &invocation{
		e: e, cmd: cmd, op: urlpipe.Operation(cmd.name), flags: a, url: normalizeURL(target),
		opts: opts, client: client, ctx: ctx, cancel: cancel,
	}, 0, true
}

// configError reports a missing key or an unreadable config file.
func (e *env) configError(err error) int {
	e.errorf("%s", err)
	if errors.Is(err, errNoKey) {
		return exitUsage
	}
	return exitAPIError
}

// accepted finishes an --async call: the token goes to stdout.
func (inv *invocation) accepted(token string, meta urlpipe.Meta) int {
	fmt.Fprintln(inv.e.stdout, token)
	inv.verbose(token, meta)
	return exitOK
}

func (inv *invocation) verbose(token string, meta urlpipe.Meta) {
	if inv.flags.verbose {
		printMeta(inv.e.stderr, token, meta)
	}
}

// fail reports an error from the client and returns the exit code.
func (inv *invocation) fail(err error) int {
	return inv.e.apiFailure(err, inv.op, inv.flags.timeout.d)
}

func textCommand(e *env, cmd *command, args []string, call func(*urlpipe.Client, context.Context, string, *urlpipe.Options) (*urlpipe.Response[string], error)) int {
	inv, code, ok := e.prepare(cmd, args, nil)
	if !ok {
		return code
	}
	defer inv.cancel()
	res, err := call(inv.client, inv.ctx, inv.url, &inv.opts)
	if err != nil {
		return inv.fail(err)
	}
	if !res.Completed() {
		return inv.accepted(res.Token, res.Meta)
	}
	writeText(e.stdout, res.Data)
	inv.verbose(res.Token, res.Meta)
	return exitOK
}

// screenshotFlags are the flags of `urlpipe screenshot`.
type screenshotFlags struct {
	output       string
	format       string
	quality      int
	width        int
	height       int
	scale        int
	viewportOnly bool
	selector     string
	dark         bool
}

func (s *screenshotFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&s.output, "o", "", "")
	fs.StringVar(&s.output, "output", "", "")
	fs.StringVar(&s.format, "format", "", "")
	fs.IntVar(&s.quality, "quality", 0, "")
	fs.IntVar(&s.width, "width", 0, "")
	fs.IntVar(&s.height, "height", 0, "")
	fs.IntVar(&s.scale, "scale", 0, "")
	fs.BoolVar(&s.viewportOnly, "viewport-only", false, "")
	fs.StringVar(&s.selector, "selector", "", "")
	fs.BoolVar(&s.dark, "dark", false, "")
}

// options is screenshot_options: only what was given is sent, and the API
// checks the ranges.
func (s *screenshotFlags) options(set map[string]bool) map[string]any {
	o := map[string]any{}
	if s.format != "" {
		o["format"] = strings.ToLower(s.format)
	}
	if set["quality"] {
		o["quality"] = s.quality
	}
	if set["width"] {
		o["viewport_width"] = s.width
	}
	if set["height"] {
		o["viewport_height"] = s.height
	}
	if set["scale"] {
		o["device_scale_factor"] = s.scale
	}
	if s.viewportOnly {
		o["full_page"] = false
	}
	if s.selector != "" {
		o["selector"] = s.selector
	}
	if s.dark {
		o["dark_mode"] = true
	}
	if len(o) == 0 {
		return nil
	}
	return o
}

func cmdScreenshot(e *env, args []string) int {
	cmd := lookup("screenshot")
	s := &screenshotFlags{}
	var fs *flag.FlagSet
	inv, code, ok := e.prepare(cmd, args, func(f *flag.FlagSet) { fs = f; s.register(f) })
	if !ok {
		return code
	}
	defer inv.cancel()
	if inv.opts.Async && s.output != "" {
		return e.usageError(cmd.name, "-o needs the image, and --async returns only a token; drop one of them")
	}
	res, err := inv.client.Screenshot(inv.ctx, inv.url, &urlpipe.ScreenshotOptions{Options: inv.opts, Screenshot: s.options(setFlags(fs))})
	if err != nil {
		return inv.fail(err)
	}
	if !res.Completed() {
		return inv.accepted(res.Token, res.Meta)
	}
	if code := saveScreenshot(e, res.Data, s.output, hostFilename(inv.url)); code != exitOK {
		return code
	}
	inv.verbose(res.Token, res.Meta)
	return exitOK
}

func cmdMeta(e *env, args []string) int {
	var asJSON bool // accepted for symmetry: meta always writes JSON
	inv, code, ok := e.prepare(lookup("meta"), args, func(fs *flag.FlagSet) { fs.BoolVar(&asJSON, "json", false, "") })
	if !ok {
		return code
	}
	defer inv.cancel()
	res, err := inv.client.Meta(inv.ctx, inv.url, &inv.opts)
	if err != nil {
		return inv.fail(err)
	}
	if !res.Completed() {
		return inv.accepted(res.Token, res.Meta)
	}
	if err := writeJSON(e.stdout, res.Data); err != nil {
		e.errorf("%s", err)
		return exitAPIError
	}
	inv.verbose(res.Token, res.Meta)
	return exitOK
}

func cmdKeywords(e *env, args []string) int {
	var asJSON bool
	inv, code, ok := e.prepare(lookup("keywords"), args, func(fs *flag.FlagSet) { fs.BoolVar(&asJSON, "json", false, "") })
	if !ok {
		return code
	}
	defer inv.cancel()
	res, err := inv.client.Keywords(inv.ctx, inv.url, &inv.opts)
	if err != nil {
		return inv.fail(err)
	}
	if !res.Completed() {
		return inv.accepted(res.Token, res.Meta)
	}
	if err := writeKeywords(e.stdout, res.Data, asJSON); err != nil {
		e.errorf("%s", err)
		return exitAPIError
	}
	inv.verbose(res.Token, res.Meta)
	return exitOK
}

func cmdConsole(e *env, args []string) int {
	var asJSON, failOnErrors bool
	cmd := lookup("console")
	inv, code, ok := e.prepare(cmd, args, func(fs *flag.FlagSet) {
		fs.BoolVar(&asJSON, "json", false, "")
		fs.BoolVar(&failOnErrors, "fail-on-errors", false, "")
	})
	if !ok {
		return code
	}
	defer inv.cancel()
	if inv.opts.Async && failOnErrors {
		return e.usageError(cmd.name, "--fail-on-errors needs the result, and --async returns only a token; drop one of them")
	}
	res, err := inv.client.Console(inv.ctx, inv.url, &inv.opts)
	if err != nil {
		return inv.fail(err)
	}
	if !res.Completed() {
		return inv.accepted(res.Token, res.Meta)
	}
	if err := writeConsole(e.stdout, res.Data, asJSON); err != nil {
		e.errorf("%s", err)
		return exitAPIError
	}
	inv.verbose(res.Token, res.Meta)
	if failOnErrors {
		if n := countErrors(res.Data); n > 0 {
			e.errorf("the page logged %d %s", n, plural(n, "error", "errors"))
			return exitCheckFailed
		}
	}
	return exitOK
}

func cmdLighthouse(e *env, args []string) int {
	var (
		asJSON, includeAudits bool
		device                string
		minScore              = -1
	)
	cmd := lookup("lighthouse")
	var fs *flag.FlagSet
	inv, code, ok := e.prepare(cmd, args, func(f *flag.FlagSet) {
		fs = f
		f.BoolVar(&asJSON, "json", false, "")
		f.BoolVar(&includeAudits, "include-audits", false, "")
		f.StringVar(&device, "device", "", "")
		f.IntVar(&minScore, "min-score", -1, "")
	})
	if !ok {
		return code
	}
	defer inv.cancel()
	if inv.flags.usesPageOptions() {
		return e.usageError(cmd.name, "lighthouse audits the page exactly as it loads, so --block-ads, --block-cookie-banners and --wait-for do not apply")
	}
	checkScore := setFlags(fs)["min-score"]
	if checkScore && (minScore < 0 || minScore > 100) {
		return e.usageError(cmd.name, "--min-score is a score from 0 to 100; got %d", minScore)
	}
	if inv.opts.Async && checkScore {
		return e.usageError(cmd.name, "--min-score needs the result, and --async returns only a token; drop one of them")
	}
	res, err := inv.client.Lighthouse(inv.ctx, inv.url, &urlpipe.LighthouseOptions{Options: inv.opts, Device: device, IncludeAudits: includeAudits})
	if err != nil {
		return inv.fail(err)
	}
	if !res.Completed() {
		return inv.accepted(res.Token, res.Meta)
	}
	if err := writeLighthouse(e.stdout, res.Data, asJSON); err != nil {
		e.errorf("%s", err)
		return exitAPIError
	}
	inv.verbose(res.Token, res.Meta)
	if checkScore {
		return e.checkMinScore(res.Data, minScore)
	}
	return exitOK
}

// checkMinScore gates on the performance score.
func (e *env) checkMinScore(l *urlpipe.Lighthouse, min int) int {
	score, ok := categoryScore(l, "performance")
	if !ok {
		e.errorf("the audit has no performance score to compare with --min-score %d", min)
		return exitCheckFailed
	}
	if score < min {
		e.errorf("the performance score %d is below %d", score, min)
		return exitCheckFailed
	}
	return exitOK
}

func categoryScore(l *urlpipe.Lighthouse, key string) (int, bool) {
	if l == nil {
		return 0, false
	}
	c := l.Categories[key]
	if c == nil || c.Score == nil {
		return 0, false
	}
	return int(math.Round(*c.Score * 100)), true
}

func cmdScrape(e *env, args []string) int {
	var (
		ops, device   string
		includeAudits bool
		asJSON        bool // accepted for symmetry: scrape always writes JSON
	)
	cmd := lookup("scrape")
	inv, code, ok := e.prepare(cmd, args, func(fs *flag.FlagSet) {
		fs.StringVar(&ops, "ops", "", "")
		fs.StringVar(&device, "device", "", "")
		fs.BoolVar(&includeAudits, "include-audits", false, "")
		fs.BoolVar(&asJSON, "json", false, "")
	})
	if !ok {
		return code
	}
	defer inv.cancel()
	var operations []urlpipe.Operation
	for _, op := range strings.Split(ops, ",") {
		if op = strings.TrimSpace(op); op != "" {
			operations = append(operations, urlpipe.Operation(strings.ToLower(op)))
		}
	}
	if len(operations) == 0 {
		return e.usageError(cmd.name, "scrape needs --ops, such as --ops markdown,meta")
	}
	res, err := inv.client.Scrape(inv.ctx, inv.url, operations, &urlpipe.ScrapeOptions{Options: inv.opts, Device: device, IncludeAudits: includeAudits})
	if err != nil {
		return inv.fail(err)
	}
	if !res.Completed() {
		return inv.accepted(res.Token, res.Meta)
	}
	if err := writeScrape(e.stdout, res.Data); err != nil {
		e.errorf("%s", err)
		return exitAPIError
	}
	reportScrapeFailures(e, res.Data, res.Token)
	inv.verbose(res.Token, res.Meta)
	return exitOK
}

// reportScrapeFailures names each operation that failed, on stderr.
func reportScrapeFailures(e *env, s *urlpipe.ScrapeResult, token string) {
	if s == nil {
		return
	}
	for _, op := range s.Order {
		entry := s.Operations[op]
		if entry.Success {
			continue
		}
		if entry.Error == "processing_timeout" {
			e.errorf("%s is still running; fetch the finished scrape with `urlpipe result %s --op scrape --wait`", op, token)
			continue
		}
		e.errorf("%s failed: %s", op, entry.Error)
	}
}

var resultOperations = []urlpipe.Operation{
	urlpipe.OperationMarkdown, urlpipe.OperationHTML, urlpipe.OperationSummarize,
	urlpipe.OperationScreenshot, urlpipe.OperationMeta, urlpipe.OperationKeywords,
	urlpipe.OperationConsole, urlpipe.OperationLighthouse, urlpipe.OperationScrape,
}

func cmdResult(e *env, args []string) int {
	cmd := lookup("result")
	fs := newFlagSet(cmd.name)
	c := &connFlags{}
	c.register(fs)
	var (
		op, output   string
		wait, asJSON bool
		interval     = durationValue{d: 2 * time.Second}
	)
	fs.StringVar(&op, "op", "", "")
	fs.BoolVar(&wait, "wait", false, "")
	fs.Var(&interval, "interval", "")
	fs.StringVar(&output, "o", "", "")
	fs.StringVar(&output, "output", "", "")
	fs.BoolVar(&asJSON, "json", false, "")
	token, code, ok := e.parseCommand(cmd, fs, args, "token")
	if !ok {
		return code
	}
	operation := urlpipe.Operation(strings.ToLower(op))
	if op != "" && !knownOperation(operation) {
		return e.usageError(cmd.name, "unknown --op %q; want one of %s", op, joinOperations(resultOperations))
	}
	if output != "" && operation != urlpipe.OperationScreenshot {
		return e.usageError(cmd.name, "-o writes an image; use it with --op screenshot")
	}
	client, err := c.client()
	if err != nil {
		return e.configError(err)
	}
	var res *urlpipe.Response[any]
	if wait {
		// --timeout bounds the wait itself, so running out of it reports the
		// token instead of cutting the request off.
		res, err = client.Wait(e.ctx, token, operation, &urlpipe.WaitOptions{Timeout: c.timeout.d, Interval: interval.d})
	} else {
		ctx, cancel := c.context(e.ctx)
		res, err = client.Result(ctx, token, operation)
		cancel()
	}
	if err != nil {
		return e.apiFailure(err, operation, c.timeout.d)
	}
	if !res.Completed() {
		e.errorf("the analysis is still running; add --wait to wait for it")
		if c.verbose {
			printMeta(e.stderr, res.Token, res.Meta)
		}
		return exitProcessing
	}
	if code := writeResult(e, operation, res, output, asJSON); code != exitOK {
		return code
	}
	if c.verbose {
		printMeta(e.stderr, res.Token, res.Meta)
	}
	return exitOK
}

func knownOperation(op urlpipe.Operation) bool {
	for _, o := range resultOperations {
		if o == op {
			return true
		}
	}
	return false
}

func joinOperations(ops []urlpipe.Operation) string {
	s := make([]string, len(ops))
	for i, op := range ops {
		s[i] = string(op)
	}
	return strings.Join(s, ", ")
}

// writeResult writes a fetched result the way the operation's own command
// would.
func writeResult(e *env, op urlpipe.Operation, res *urlpipe.Response[any], output string, asJSON bool) int {
	var err error
	switch data := res.Data.(type) {
	case string:
		writeText(e.stdout, data)
	case *urlpipe.Screenshot:
		return saveScreenshot(e, data, output, res.Token)
	case *urlpipe.Metadata:
		err = writeJSON(e.stdout, data)
	case []string:
		err = writeKeywords(e.stdout, data, asJSON)
	case []urlpipe.ConsoleEntry:
		err = writeConsole(e.stdout, data, asJSON)
	case *urlpipe.Lighthouse:
		err = writeLighthouse(e.stdout, data, asJSON)
	case *urlpipe.ScrapeResult:
		if err = writeScrape(e.stdout, data); err == nil {
			reportScrapeFailures(e, data, res.Token)
		}
	default:
		err = writeJSON(e.stdout, data)
	}
	if err != nil {
		e.errorf("%s", err)
		return exitAPIError
	}
	return exitOK
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func cmdVersion(e *env, args []string) int {
	fs := newFlagSet("version")
	if _, code, ok := e.parseCommand(lookup("version"), fs, args, ""); !ok {
		return code
	}
	fmt.Fprintln(e.stdout, "urlpipe "+cliVersion())
	return exitOK
}

func cmdHelp(e *env, args []string) int {
	switch len(args) {
	case 0:
		fmt.Fprint(e.stdout, mainHelp())
		return exitOK
	case 1:
		if cmd := lookup(args[0]); cmd != nil {
			fmt.Fprint(e.stdout, commandHelp(cmd))
			return exitOK
		}
		return e.usageError("", "unknown command %q", args[0])
	}
	return e.usageError("help", "help takes one command; got %d", len(args))
}
