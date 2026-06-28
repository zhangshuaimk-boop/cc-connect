package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func TestAgentBuildManifestCoversAgentPackages(t *testing.T) {
	root := repoRoot(t)
	agentsDir := filepath.Join(root, "agent")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		t.Fatalf("read agent dir: %v", err)
	}

	var agents []string
	for _, entry := range entries {
		if entry.IsDir() {
			agents = append(agents, entry.Name())
		}
	}
	slices.Sort(agents)

	makefileAgents := parseAllAgents(t, filepath.Join(root, "Makefile"))
	for _, agent := range agents {
		if !slices.Contains(makefileAgents, agent) {
			t.Errorf("Makefile ALL_AGENTS missing %q", agent)
		}
		if _, err := os.Stat(filepath.Join(root, "cmd", "cc-connect", "plugin_agent_"+agent+".go")); err != nil {
			t.Errorf("missing plugin import for agent %q: %v", agent, err)
		}
	}
}

func TestRegisteredAgentsCoverBuildManifest(t *testing.T) {
	registered := core.ListRegisteredAgents()
	slices.Sort(registered)

	for _, agent := range parseAllAgents(t, filepath.Join(repoRoot(t), "Makefile")) {
		if !slices.Contains(registered, agent) {
			t.Errorf("registered agents missing %q; check plugin_agent_%s.go", agent, agent)
		}
	}
}

func TestTraexAgentIsRegisteredForStartup(t *testing.T) {
	agent, err := core.CreateAgent("traex", map[string]any{"cmd": "true"})
	if err != nil {
		t.Fatalf("CreateAgent(traex): %v", err)
	}
	if agent.Name() != "traex" {
		t.Fatalf("agent name = %q, want traex", agent.Name())
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func parseAllAgents(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	re := regexp.MustCompile(`(?m)^ALL_AGENTS\s*:=\s*(.+)$`)
	match := re.FindSubmatch(data)
	if match == nil {
		t.Fatal("ALL_AGENTS not found in Makefile")
	}
	fields := strings.Fields(string(match[1]))
	slices.Sort(fields)
	return fields
}
