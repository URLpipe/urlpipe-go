// Command urlpipe turns a URL into Markdown, rendered HTML, a screenshot,
// metadata, a summary, keywords, console errors or a Lighthouse audit, from
// the terminal. It is built on the github.com/URLpipe/urlpipe-go package.
//
// Install it with:
//
//	go install github.com/URLpipe/urlpipe-go/cmd/urlpipe@latest
//
// Run `urlpipe help` for the commands and their flags.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
)

// version is set at build time with -ldflags "-X main.version=1.2.3".
var version = "dev"

// Exit codes. They are part of the interface scripts rely on.
const (
	exitOK          = 0
	exitAPIError    = 1 // the API or the network failed
	exitUsage       = 2 // the command line is wrong, or there is no API key
	exitCheckFailed = 3 // --fail-on-errors or --min-score found a problem
	exitProcessing  = 4 // `urlpipe result` without --wait: still running
	exitInterrupted = 130
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := runContext(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run is the whole program: it parses args, talks to the API and writes to
// stdout and stderr, returning the exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, stdin, stdout, stderr)
}

func runContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	e := &env{ctx: ctx, stdin: stdin, stdout: stdout, stderr: stderr}
	if len(args) == 0 {
		fmt.Fprint(stderr, mainHelp())
		return exitUsage
	}
	name, rest := args[0], args[1:]
	switch name {
	case "-h", "-help", "--help":
		fmt.Fprint(stdout, mainHelp())
		return exitOK
	case "--version":
		return cmdVersion(e, rest)
	}
	cmd := lookup(name)
	if cmd == nil {
		return e.usageError("", "unknown command %q", name)
	}
	return cmd.run(e, rest)
}

// cliVersion is the version injected at build time, else the module version
// `go install ...@v1.2.3` records, else "dev".
func cliVersion() string {
	if version != "dev" && version != "" {
		return strings.TrimPrefix(version, "v")
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return "dev"
}

// env is what a command reads from and writes to.
type env struct {
	ctx    context.Context
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// errorf prints "urlpipe: <message>" to stderr.
func (e *env) errorf(format string, a ...any) {
	fmt.Fprintf(e.stderr, "urlpipe: "+format+"\n", a...)
}

// usageError reports a wrong command line and points at the help.
func (e *env) usageError(command, format string, a ...any) int {
	e.errorf(format, a...)
	if command == "" {
		fmt.Fprintln(e.stderr, "Run 'urlpipe help' for the commands.")
	} else {
		fmt.Fprintf(e.stderr, "Run 'urlpipe help %s' for its flags.\n", command)
	}
	return exitUsage
}
