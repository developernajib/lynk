package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadDotenv loads KEY=VALUE pairs from the given files (".env" when none are
// given) into the process environment. Variables already present in the real
// environment always win, so a .env file can never override production
// settings. Missing files are ignored; an unreadable file is an error.
//
// The format is deliberately minimal: one KEY=VALUE per line, blank lines and
// #-comments skipped, an optional "export " prefix, and optional matching
// single or double quotes around the value. No multiline values, no variable
// expansion.
func LoadDotenv(paths ...string) error {
	if len(paths) == 0 {
		paths = []string{".env"}
	}
	for _, path := range paths {
		if err := loadDotenvFile(path); err != nil {
			return err
		}
	}
	return nil
}

func loadDotenvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("config: open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := parseDotenvLine(scanner.Text())
		if !ok {
			continue
		}
		// LookupEnv distinguishes "unset" from "set to empty": an explicitly
		// empty real environment variable still wins over the file.
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("config: set %s: %w", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	return nil
}

// parseDotenvLine extracts a key/value pair, reporting ok=false for blank
// lines, comments, and anything malformed (skipped, never fatal).
func parseDotenvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")

	key, value, found := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	if !found || key == "" {
		return "", "", false
	}

	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, true
}
