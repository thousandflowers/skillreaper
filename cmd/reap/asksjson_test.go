package main

import "testing"

func TestAsksJSONReadsEveryFormTheFlagPackageTakes(t *testing.T) {
	for arg, want := range map[string]bool{
		"--json": true, "-json": true, "--json=true": true, "-json=1": true, "--json=t": true,
		"--json=false": false, "--json=0": false, "--jsonx": false, "json": false, "--md": false, "--json=maybe": false,
	} {
		if got := asksJSON(arg); got != want {
			t.Errorf("asksJSON(%q) = %v, want %v", arg, got, want)
		}
	}
	if hintRewriter("skillreaper", []string{"--json=true"}) != nil {
		t.Error("--json=true still rewrites the JSON")
	}
}
