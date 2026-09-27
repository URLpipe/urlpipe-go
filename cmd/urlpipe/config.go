package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// config is what `urlpipe login` saves.
type config struct {
	APIKey string `json:"api_key"`
}

// configPath is <user config dir>/urlpipe/config.json. URLPIPE_CONFIG_DIR
// overrides the directory, which the tests use.
func configPath() (string, error) {
	if dir := os.Getenv("URLPIPE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "config.json"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding your config directory: %w", err)
	}
	return filepath.Join(dir, "urlpipe", "config.json"), nil
}

// describeConfigPath is the path for the help text, or a description of it
// when it can't be worked out.
func describeConfigPath() string {
	if p, err := configPath(); err == nil {
		return p
	}
	return "urlpipe/config.json in your user config directory"
}

// loadConfig reads the saved config. A missing file is an empty config.
func loadConfig() (config, string, error) {
	var cfg config
	path, err := configPath()
	if err != nil {
		return cfg, "", err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, path, nil
	}
	if err != nil {
		return cfg, path, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, path, fmt.Errorf("reading %s: %w (run `urlpipe login` to rewrite it)", path, err)
	}
	return cfg, path, nil
}

// saveConfig writes the config readable only by its owner.
func saveConfig(cfg config) (string, error) {
	path, err := configPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return path, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return path, err
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return path, fmt.Errorf("writing %s: %w", path, err)
	}
	// WriteFile keeps the mode of a file that already exists.
	if err := os.Chmod(path, 0o600); err != nil {
		return path, fmt.Errorf("securing %s: %w", path, err)
	}
	return path, nil
}

func cmdLogin(e *env, args []string) int {
	cmd := lookup("login")
	flags := newFlagSet(cmd.name)
	var key string
	flags.StringVar(&key, "api-key", "", "")
	if _, code, ok := e.parseCommand(cmd, flags, args, ""); !ok {
		return code
	}
	if strings.TrimSpace(key) == "" {
		// The key is read as a line: the standard library has no portable way
		// to turn terminal echo off, so it shows as you paste it. Pipe it in
		// (echo "$KEY" | urlpipe login) to keep it off the screen.
		fmt.Fprint(e.stderr, "Paste your project's API key (it is shown as you paste; pipe it in to avoid that): ")
		line, err := bufio.NewReader(e.stdin).ReadString('\n')
		if err != nil && strings.TrimSpace(line) == "" {
			fmt.Fprintln(e.stderr)
			return e.usageError(cmd.name, "no API key was entered")
		}
		key = line
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return e.usageError(cmd.name, "no API key was entered")
	}
	path, err := saveConfig(config{APIKey: key})
	if err != nil {
		e.errorf("%s", err)
		return exitAPIError
	}
	fmt.Fprintf(e.stderr, "Saved your API key to %s.\n", path)
	return exitOK
}

func cmdLogout(e *env, args []string) int {
	cmd := lookup("logout")
	flags := newFlagSet(cmd.name)
	if _, code, ok := e.parseCommand(cmd, flags, args, ""); !ok {
		return code
	}
	path, err := configPath()
	if err != nil {
		e.errorf("%s", err)
		return exitAPIError
	}
	err = os.Remove(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		fmt.Fprintln(e.stderr, "No API key was saved.")
	case err != nil:
		e.errorf("removing %s: %s", path, err)
		return exitAPIError
	default:
		fmt.Fprintf(e.stderr, "Removed the API key saved in %s.\n", path)
	}
	return exitOK
}
