package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runCombined executes a command and returns combined stdout+stderr.
// Errors include the command output for debugging.
func runCombined(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w — %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runCombinedOut executes a command and returns combined stdout+stderr as a string.
// Errors include the command output for debugging.
func runCombinedOut(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w — %s", name, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// runJSON executes a command, decodes stdout as JSON into v, and returns
// any error (including JSON parse failures). Stderr is included in the
// error message when the command fails.
func runJSON(v any, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w — %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return json.Unmarshal(stdout.Bytes(), v)
}

// jsonPrint pretty-prints v as indented JSON to stdout.
func jsonPrint(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
