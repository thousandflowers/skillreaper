package main

import (
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// One config file declares a server in several projects under one name: each
// is a server of its own, and each is listed, not the first one a map hands out.
func TestServersOfOneNameInSeveralScopesAreEachListed(t *testing.T) {
	items := []scan.Item{
		{Category: scan.CatMCP, Platform: "claude-code", Path: "/h/.claude.json", Name: "fs", Source: "project:/p/a"},
		{Category: scan.CatMCP, Platform: "claude-code", Path: "/h/.claude.json", Name: "fs", Source: "project:/p/b"},
		{Category: scan.CatMCP, Platform: "claude-code", Path: "/h/.claude.json", Name: "fs", Source: "user-config"},
		{Category: scan.CatMCP, Platform: "claude-code", Path: "/h/.claude.json", Name: "fs", Source: "user-config"},
	}
	got := dedupeByPath(items)
	var sources []string
	for _, it := range got {
		sources = append(sources, it.Source)
	}
	if strings.Join(sources, ",") != "project:/p/a,project:/p/b,user-config" {
		t.Errorf("listed %v", sources)
	}
}
