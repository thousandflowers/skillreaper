package main

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// reportTokens runs a JSON report over the fixture with the given extra flags
// and returns the token count of every row, keyed by name.
func reportTokens(t *testing.T, claudeDir, claudeJSON string, extra ...string) map[string]int {
	t.Helper()
	var out, errOut bytes.Buffer
	args := append([]string{
		"--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--json",
	}, extra...)

	if code := run(args, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut.String())
	}
	var decoded struct {
		Rows []struct {
			Name   string
			Tokens int
		}
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	byName := make(map[string]int, len(decoded.Rows))
	for _, r := range decoded.Rows {
		byName[r.Name] = r.Tokens
	}
	if len(byName) == 0 {
		t.Fatal("fixture produced no rows — cannot compare ratios")
	}
	return byName
}

// TestModelFlagReachesTokenCounts is the end-to-end half of the tokenizer
// change: it drives the real CLI, so it fails if the Model field is ever
// dropped from gather()'s report.Opts literal. Both the cost-package tests and
// report.TestBuildThreadsModelIntoTokens still pass with that wiring removed,
// which is exactly why this one exists.
func TestModelFlagReachesTokenCounts(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)

	def := reportTokens(t, claudeDir, claudeJSON)
	openai := reportTokens(t, claudeDir, claudeJSON, "--model", "gpt-4o")

	if len(def) != len(openai) {
		t.Fatalf("row count changed with --model: %d vs %d", len(def), len(openai))
	}

	sawDifference := false
	for name, defTok := range def {
		gotTok, ok := openai[name]
		if !ok {
			t.Fatalf("row %q missing from the --model report", name)
		}
		// 3.7 → 4.0 chars per token: the OpenAI count is the default scaled by
		// 37/40, allowing one token of slack for the two independent ceilings.
		want := int(math.Ceil(float64(defTok) * 37.0 / 40.0))
		if gotTok != want && gotTok != want-1 {
			t.Errorf("row %q: tokens = %d with --model gpt-4o, want ~%d (default %d)",
				name, gotTok, want, defTok)
		}
		if gotTok != defTok {
			sawDifference = true
		}
	}

	// Without this the test would still pass if --model were ignored entirely
	// and every row happened to be tiny enough to round to the same value.
	if !sawDifference {
		t.Error("no row changed with --model gpt-4o — the flag is not reaching the token estimate")
	}
}
