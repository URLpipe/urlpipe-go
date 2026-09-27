package main

import (
	"fmt"
	"strings"
)

// sharedFlagsHelp is the help for the flags every analysis command takes.
const sharedFlagsHelp = `Shared flags:
      --api-key KEY       Your project's API key; else URLPIPE_API_KEY, else the
                          key saved by 'urlpipe login'
      --max-age AGE       How old a cached result may be: 3d, 2h, 30m, a number of
                          seconds, or 0 for a fresh visit (default 7 days)
      --label KEY=VALUE   Tag the request with your own id; repeat for more
      --residential       Fetch from a residential (home broadband) address
      --async             Don't wait: print the result's token and exit
      --block-ads         Block ads before reading the page
      --block-cookie-banners
                          Remove cookie banners before reading the page
      --wait-for CSS      Wait up to 10 s for this element before reading the page
      --timeout D         Give up after this long, such as 90s or 5m
  -v, --verbose           Print the token, cache status, cost, credits left and
                          time taken to stderr
`

// connectionFlagsHelp is the help for commands that call the API but don't
// run an analysis.
const connectionFlagsHelp = `Shared flags:
      --api-key KEY       Your project's API key; else URLPIPE_API_KEY, else the
                          key saved by 'urlpipe login'
      --timeout D         Give up after this long, such as 90s or 5m
  -v, --verbose           Print the token, cache status, cost, credits left and
                          time taken to stderr
`

const exitCodesHelp = `Exit codes:
  0  success
  1  the API or the network failed
  2  the command line is wrong, or there is no API key
  3  --fail-on-errors or --min-score found a problem
  4  urlpipe result: the analysis is still running (add --wait)
`

func mainHelp() string {
	var b strings.Builder
	b.WriteString("urlpipe turns any URL into Markdown, rendered HTML, a screenshot, metadata,\n")
	b.WriteString("keywords, a summary, console errors or a Lighthouse audit, from the terminal.\n")
	b.WriteString("Pages are rendered in real Chrome, so JavaScript-heavy sites work.\n\n")
	b.WriteString("Usage:\n  urlpipe <command> [arguments] [flags]\n\nCommands:\n")
	width := 0
	for _, c := range commands {
		if len(c.name) > width {
			width = len(c.name)
		}
	}
	for _, c := range commands {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, c.name, c.summary)
	}
	b.WriteString("\nExamples:\n")
	b.WriteString("  urlpipe markdown example.com > page.md\n")
	b.WriteString("  urlpipe screenshot https://example.com --width 390 --scale 2\n")
	b.WriteString("  urlpipe console \"$URL\" --fail-on-errors\n")
	b.WriteString("  urlpipe lighthouse \"$URL\" --device desktop --min-score 90\n\n")
	b.WriteString(exitCodesHelp)
	b.WriteString("\nRun 'urlpipe help <command>' for a command's flags. Docs: https://urlpipe.dev/docs\n")
	return b.String()
}

func commandHelp(c *command) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage:\n  urlpipe %s\n\n%s\n", c.synopsis, c.about)
	if c.flags != "" {
		b.WriteString("\nFlags:\n")
		b.WriteString(c.flags)
	}
	switch c.kind {
	case allAnalysisFlags:
		b.WriteString("\n" + sharedFlagsHelp)
	case connectionFlags:
		b.WriteString("\n" + connectionFlagsHelp)
	}
	return b.String()
}
