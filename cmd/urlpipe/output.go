package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/URLpipe/urlpipe-go"
)

// writeText writes a text result, ending it with a newline when it has none.
func writeText(w io.Writer, s string) {
	io.WriteString(w, s)
	if s != "" && !strings.HasSuffix(s, "\n") {
		io.WriteString(w, "\n")
	}
}

// writeJSON writes v as JSON indented by two spaces, then a newline.
func writeJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the result: %w", err)
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// writeRawJSON indents JSON that is already encoded.
func writeRawJSON(w io.Writer, raw []byte) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return fmt.Errorf("encoding the result: %w", err)
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
}

func writeKeywords(w io.Writer, keywords []string, asJSON bool) error {
	if asJSON {
		if keywords == nil {
			keywords = []string{}
		}
		return writeJSON(w, keywords)
	}
	for _, k := range keywords {
		fmt.Fprintln(w, k)
	}
	return nil
}

// oneLine keeps a console message on one line.
var oneLine = strings.NewReplacer("\r\n", `\n`, "\n", `\n`, "\r", `\n`)

func writeConsole(w io.Writer, entries []urlpipe.ConsoleEntry, asJSON bool) error {
	if asJSON {
		if entries == nil {
			entries = []urlpipe.ConsoleEntry{}
		}
		return writeJSON(w, entries)
	}
	for _, en := range entries {
		fmt.Fprintf(w, "%s: %s\n", en.Type, oneLine.Replace(en.Text))
	}
	return nil
}

// countErrors counts the entries --fail-on-errors fails on: errors and
// exceptions, not warnings.
func countErrors(entries []urlpipe.ConsoleEntry) int {
	n := 0
	for _, en := range entries {
		if en.Type == urlpipe.ConsoleError || en.Type == urlpipe.ConsoleException {
			n++
		}
	}
	return n
}

// extension is the file extension for an image's MIME type.
func extension(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ".png"
}

// hostFilename is the default screenshot name: the page's host, such as
// example.com, which saveScreenshot gives its extension.
func hostFilename(pageURL string) string {
	u, err := url.Parse(pageURL)
	if err != nil || u.Hostname() == "" {
		return "screenshot"
	}
	return u.Hostname()
}

// saveScreenshot writes the image to output: "-" is stdout, "" is base plus
// the image's extension. The path, then the image's key-free link, go to
// stderr.
func saveScreenshot(e *env, shot *urlpipe.Screenshot, output, base string) int {
	if shot == nil || len(shot.Data) == 0 {
		e.errorf("the response carried no image")
		return exitAPIError
	}
	if output == "-" {
		if _, err := e.stdout.Write(shot.Data); err != nil {
			e.errorf("writing the image: %s", err)
			return exitAPIError
		}
		return exitOK
	}
	if output == "" {
		output = base + extension(shot.MIMEType)
	}
	if err := os.WriteFile(output, shot.Data, 0o644); err != nil {
		e.errorf("writing %s: %s", output, err)
		return exitAPIError
	}
	fmt.Fprintln(e.stderr, output)
	if shot.ResultURL != "" {
		fmt.Fprintln(e.stderr, shot.ResultURL)
	}
	return exitOK
}

// The four categories Lighthouse scores, and the metrics worth a line, in the
// order they are written. Anything else the audit reports follows, sorted.
var (
	lighthouseCategories = []string{"performance", "accessibility", "best-practices", "seo"}
	lighthouseMetrics    = []string{
		"first-contentful-paint", "largest-contentful-paint", "total-blocking-time",
		"cumulative-layout-shift", "speed-index", "interactive",
	}
)

// writeLighthouse writes the summary, or the whole audit with asJSON.
func writeLighthouse(w io.Writer, l *urlpipe.Lighthouse, asJSON bool) error {
	if l == nil {
		return fmt.Errorf("the response carried no audit")
	}
	if asJSON {
		if len(l.Raw) > 0 {
			return writeRawJSON(w, l.Raw)
		}
		return writeJSON(w, l)
	}
	var cats []string
	for _, k := range ordered(lighthouseCategories, keysOf(l.Categories)) {
		c := l.Categories[k]
		value := "-"
		if c != nil && c.Score != nil {
			value = strconv.Itoa(int(math.Round(*c.Score * 100)))
		}
		cats = append(cats, row(k, value))
	}
	var metrics []string
	for _, k := range ordered(lighthouseMetrics, keysOf(l.Metrics)) {
		m := l.Metrics[k]
		value := "-"
		if m != nil && m.DisplayValue != "" {
			value = strings.ReplaceAll(m.DisplayValue, " ", " ")
		}
		metrics = append(metrics, row(k, value))
	}
	out := strings.Join(cats, "")
	if len(metrics) > 0 {
		out += "\n" + strings.Join(metrics, "")
	}
	_, err := io.WriteString(w, out)
	return err
}

func row(key, value string) string { return fmt.Sprintf("%-26s%s\n", key, value) }

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ordered lists first the keys of want that are present, then the rest,
// sorted.
func ordered(want, have []string) []string {
	present := map[string]bool{}
	for _, k := range have {
		present[k] = true
	}
	var out []string
	for _, k := range want {
		if present[k] {
			out = append(out, k)
			delete(present, k)
		}
	}
	var rest []string
	for k := range present {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// writeScrape writes the combined result as indented JSON, the operations in
// the order they were asked for (a Go map would sort them).
func writeScrape(w io.Writer, s *urlpipe.ScrapeResult) error {
	if s == nil {
		return fmt.Errorf("the response carried no result")
	}
	var buf bytes.Buffer
	u, _ := json.Marshal(s.URL)
	buf.WriteString(`{"url":`)
	buf.Write(u)
	buf.WriteString(`,"operations":{`)
	for i, op := range s.Order {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(string(op))
		v, err := json.Marshal(s.Operations[op])
		if err != nil {
			return fmt.Errorf("encoding the result: %w", err)
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteString("}}")
	return writeRawJSON(w, buf.Bytes())
}
