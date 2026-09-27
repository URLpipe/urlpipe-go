package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A 1x1 PNG.
var pngBytes, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=")

// stub is a fake API: it answers each path with a canned response and records
// what it was sent.
type stub struct {
	t *testing.T
	*httptest.Server
	mu       sync.Mutex
	requests []recorded
	handle   func(w http.ResponseWriter, r *http.Request, body map[string]any)
}

type recorded struct {
	path, auth, userAgent string
	body                  map[string]any
}

func newStub(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, body map[string]any)) *stub {
	s := &stub{t: t, handle: handle}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.requests = append(s.requests, recorded{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("User-Agent"), body})
		s.mu.Unlock()
		s.handle(w, r, body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *stub) last() recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		s.t.Fatal("no request reached the stub")
	}
	return s.requests[len(s.requests)-1]
}

// api answers like the real API for the operations the tests use.
func api(w http.ResponseWriter, r *http.Request, body map[string]any) {
	w.Header().Set("X-Result-Token", "tok_123")
	w.Header().Set("X-Cache", "miss")
	w.Header().Set("X-Quota-Cost", "1")
	w.Header().Set("X-Quota-Remaining", "999")
	if body["sync"] != true {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"token":"tok_123","status":"accepted","labels":{}}`)
		return
	}
	switch r.URL.Path {
	case "/markdown":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "# Example Domain\n\nBody text.")
	case "/screenshot":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Result-Url", "https://urlpipe.dev/r/tok_123.png")
		io.WriteString(w, base64.StdEncoding.EncodeToString(pngBytes))
	case "/console":
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[{"type":"warning","text":"old API"},{"type":"error","text":"boom\nat line 2"}]`)
	case "/lighthouse":
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"url":"https://example.com","device":"mobile","categories":{"performance":{"score":0.72},"accessibility":{"score":1},"best-practices":{"score":0.96},"seo":{"score":null}},"metrics":{"largest-contentful-paint":{"displayValue":"2.5 s"},"cumulative-layout-shift":{"displayValue":"0.01"}}}`)
	case "/keywords":
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `["pipes","urls"]`)
	default:
		w.WriteHeader(404)
		io.WriteString(w, `{"error":"not_found"}`)
	}
}

// cli runs the program in-process with a fresh config dir and no key in the
// environment unless the test sets one.
func cli(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// chdir moves into dir for the test (testing.T.Chdir needs Go 1.24).
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("URLPIPE_CONFIG_DIR", dir)
	t.Setenv("URLPIPE_API_KEY", "")
	return dir
}

func TestMarkdownWritesTheResultToStdoutAndMetaToStderr(t *testing.T) {
	isolate(t)
	s := newStub(t, api)
	code, out, errOut := cli(t, "", "markdown", "example.com", "--api-key", "k1", "--base-url", s.URL, "-v")

	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if out != "# Example Domain\n\nBody text.\n" {
		t.Fatalf("stdout %q", out)
	}
	if !strings.Contains(errOut, "cache miss") || !strings.Contains(errOut, "remaining 999") {
		t.Fatalf("verbose meta missing from stderr: %q", errOut)
	}
	got := s.last()
	if got.path != "/markdown" || got.auth != "Bearer k1" || got.body["url"] != "https://example.com" || got.body["sync"] != true {
		t.Fatalf("request %+v", got)
	}
	if !strings.HasPrefix(got.userAgent, "urlpipe-cli/") {
		t.Fatalf("User-Agent %q", got.userAgent)
	}
}

func TestFlagsMayComeBeforeOrAfterTheURL(t *testing.T) {
	isolate(t)
	s := newStub(t, api)
	for _, args := range [][]string{
		{"markdown", "--api-key", "k", "--base-url", s.URL, "--label", "client=acme", "https://example.com"},
		{"markdown", "https://example.com", "--label", "client=acme", "--api-key", "k", "--base-url", s.URL},
	} {
		if code, _, errOut := cli(t, "", args...); code != exitOK {
			t.Fatalf("%v: exit %d, %s", args, code, errOut)
		}
		labels, _ := s.last().body["labels"].(map[string]any)
		if labels["client"] != "acme" {
			t.Fatalf("%v: labels %v", args, s.last().body["labels"])
		}
	}
}

func TestAsyncPrintsTheTokenOnly(t *testing.T) {
	isolate(t)
	s := newStub(t, api)
	code, out, _ := cli(t, "", "markdown", "example.com", "--async", "--api-key", "k", "--base-url", s.URL)
	if code != exitOK || out != "tok_123\n" {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	if s.last().body["sync"] != false {
		t.Fatalf("sync %v", s.last().body["sync"])
	}
}

func TestScreenshotSavesToAFileNamedAfterTheHost(t *testing.T) {
	isolate(t)
	s := newStub(t, api)
	chdir(t, t.TempDir())
	code, out, errOut := cli(t, "", "screenshot", "https://example.com/page", "--dark", "--width", "390", "--api-key", "k", "--base-url", s.URL)

	if code != exitOK {
		t.Fatalf("exit %d, %s", code, errOut)
	}
	if out != "" {
		t.Fatalf("stdout should be empty, got %q", out)
	}
	b, err := os.ReadFile("example.com.png")
	if err != nil || !bytes.Equal(b, pngBytes) {
		t.Fatalf("file: %v", err)
	}
	if !strings.Contains(errOut, "example.com.png") || !strings.Contains(errOut, "https://urlpipe.dev/r/tok_123.png") {
		t.Fatalf("stderr %q", errOut)
	}
	opts, _ := s.last().body["screenshot_options"].(map[string]any)
	if opts["dark_mode"] != true || opts["viewport_width"] != float64(390) {
		t.Fatalf("screenshot_options %v", s.last().body["screenshot_options"])
	}
}

func TestScreenshotToStdout(t *testing.T) {
	isolate(t)
	s := newStub(t, api)
	code, out, _ := cli(t, "", "screenshot", "example.com", "-o", "-", "--api-key", "k", "--base-url", s.URL)
	if code != exitOK || out != string(pngBytes) {
		t.Fatalf("exit %d, %d bytes", code, len(out))
	}
}

func TestConsoleKeepsEachEntryOnOneLine(t *testing.T) {
	isolate(t)
	s := newStub(t, api)
	code, out, _ := cli(t, "", "console", "example.com", "--api-key", "k", "--base-url", s.URL)
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if out != "warning: old API\nerror: boom\\nat line 2\n" {
		t.Fatalf("stdout %q", out)
	}
}

func TestConsoleFailOnErrorsExitsThree(t *testing.T) {
	isolate(t)
	s := newStub(t, api)
	code, _, errOut := cli(t, "", "console", "example.com", "--fail-on-errors", "--api-key", "k", "--base-url", s.URL)
	if code != exitCheckFailed || !strings.Contains(errOut, "logged 1 error") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestLighthouseSummaryAndMinScore(t *testing.T) {
	isolate(t)
	s := newStub(t, api)
	code, out, _ := cli(t, "", "lighthouse", "example.com", "--api-key", "k", "--base-url", s.URL)
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	want := "performance               72\naccessibility             100\nbest-practices            96\nseo                       -\n\n" +
		"largest-contentful-paint  2.5 s\ncumulative-layout-shift   0.01\n"
	if out != want {
		t.Fatalf("stdout\n%s\nwant\n%s", out, want)
	}

	code, _, errOut := cli(t, "", "lighthouse", "example.com", "--min-score", "90", "--api-key", "k", "--base-url", s.URL)
	if code != exitCheckFailed || !strings.Contains(errOut, "72 is below 90") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if code, _, _ := cli(t, "", "lighthouse", "example.com", "--min-score", "70", "--api-key", "k", "--base-url", s.URL); code != exitOK {
		t.Fatalf("a score above the minimum exited %d", code)
	}
}

func TestKeywordsAsLinesOrJSON(t *testing.T) {
	isolate(t)
	s := newStub(t, api)
	if _, out, _ := cli(t, "", "keywords", "example.com", "--api-key", "k", "--base-url", s.URL); out != "pipes\nurls\n" {
		t.Fatalf("lines %q", out)
	}
	if _, out, _ := cli(t, "", "keywords", "example.com", "--json", "--api-key", "k", "--base-url", s.URL); !strings.Contains(out, `"pipes"`) {
		t.Fatalf("json %q", out)
	}
}

func TestAnAuthenticationErrorExitsOneWithAHint(t *testing.T) {
	isolate(t)
	s := newStub(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		io.WriteString(w, `{"error":"invalid_api_key","message":"The API key is not valid."}`)
	})
	code, out, errOut := cli(t, "", "markdown", "example.com", "--api-key", "bad", "--base-url", s.URL)
	if code != exitAPIError || out != "" {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	if !strings.Contains(errOut, "urlpipe: The API key is not valid.") || !strings.Contains(errOut, "urlpipe login") {
		t.Fatalf("stderr %q", errOut)
	}
}

func TestNoKeyAnywhereIsAUsageError(t *testing.T) {
	isolate(t)
	code, _, errOut := cli(t, "", "markdown", "example.com", "--base-url", "http://127.0.0.1:1")
	if code != exitUsage || !strings.Contains(errOut, "no API key") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestTheKeyComesFromTheFlagThenTheEnvironmentThenTheConfig(t *testing.T) {
	isolate(t)
	s := newStub(t, api)
	if code, _, _ := cli(t, "sk-config\n", "login"); code != exitOK {
		t.Fatalf("login exit %d", code)
	}
	cli(t, "", "markdown", "example.com", "--base-url", s.URL)
	if got := s.last().auth; got != "Bearer sk-config" {
		t.Fatalf("config key: %q", got)
	}
	t.Setenv("URLPIPE_API_KEY", "sk-env")
	cli(t, "", "markdown", "example.com", "--base-url", s.URL)
	if got := s.last().auth; got != "Bearer sk-env" {
		t.Fatalf("env key: %q", got)
	}
	cli(t, "", "markdown", "example.com", "--api-key", "sk-flag", "--base-url", s.URL)
	if got := s.last().auth; got != "Bearer sk-flag" {
		t.Fatalf("flag key: %q", got)
	}
}

func TestLoginSavesAPrivateFileAndLogoutRemovesIt(t *testing.T) {
	dir := isolate(t)
	if code, _, errOut := cli(t, "", "login", "--api-key", "sk-1"); code != exitOK {
		t.Fatalf("login exit %d, %s", code, errOut)
	}
	path := filepath.Join(dir, "config.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode %o", perm)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"sk-1"`) {
		t.Fatalf("config %s", b)
	}
	if code, _, _ := cli(t, "", "logout"); code != exitOK {
		t.Fatalf("logout exit %d", code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("config still there: %v", err)
	}
}

func TestUsageErrors(t *testing.T) {
	isolate(t)
	for _, args := range [][]string{
		{"frobnicate"},
		{"markdown"},
		{"markdown", "a.com", "b.com"},
		{"markdown", "a.com", "--no-such-flag"},
		{"scrape", "a.com", "--api-key", "k"},
		{"lighthouse", "a.com", "--min-score", "120", "--api-key", "k"},
		{},
	} {
		if code, _, _ := cli(t, "", args...); code != exitUsage {
			t.Fatalf("%v: exit %d, want %d", args, code, exitUsage)
		}
	}
}

func TestHelpAndVersion(t *testing.T) {
	if code, out, _ := cli(t, "", "help"); code != exitOK || !strings.Contains(out, "screenshot") || !strings.Contains(out, "Exit codes") {
		t.Fatalf("help: %d %q", code, out)
	}
	if code, out, _ := cli(t, "", "screenshot", "--help"); code != exitOK || !strings.Contains(out, "--viewport-only") || !strings.Contains(out, "--max-age") {
		t.Fatalf("screenshot --help: %d %q", code, out)
	}
	if code, out, _ := cli(t, "", "version"); code != exitOK || !strings.HasPrefix(out, "urlpipe ") {
		t.Fatalf("version: %d %q", code, out)
	}
}
