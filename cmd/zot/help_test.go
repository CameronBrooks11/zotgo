package main

import (
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestRootHelpRoutesDiscovery(t *testing.T) {
	stdout, stderr, err := runCLI("http://unused.invalid", "--help")
	if err != nil {
		t.Fatalf("help: %v\nstderr: %s", err, stderr)
	}
	for _, want := range []string{
		"Start with `zot doctor`",
		"zot <command> --help",
		"Get started:",
		"Find and inspect:",
		"Item details:",
		"Export:",
		"Organize collections:",
		"Modify (local only):",
		"Authorize automation:",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("root help missing %q:\n%s", want, stdout)
		}
	}
}

func TestDiscoveryTracksEveryTopLevelCommand(t *testing.T) {
	expected := map[string]string{
		"doctor":      "Get started",
		"list":        "Find and inspect",
		"show":        "Find and inspect",
		"search":      "Find and inspect",
		"collections": "Find and inspect",
		"stats":       "Find and inspect",
		"attachment":  "Item details",
		"note":        "Item details",
		"relation":    "Item details",
		"annotation":  "Item details",
		"export":      "Export",
		"collection":  "Organize collections",
		"item":        "Modify (local only)",
		"tag":         "Modify (local only)",
		"grant":       "Authorize automation",
	}
	root := rootCommand()
	if !root.Suggest {
		t.Error("root suggestions are disabled")
	}
	if len(root.Commands) != len(expected) {
		t.Fatalf("top-level command count = %d, want %d", len(root.Commands), len(expected))
	}
	for _, command := range root.Commands {
		category, ok := expected[command.Name]
		if !ok {
			t.Errorf("unexpected top-level command %q", command.Name)
			continue
		}
		if command.Category != category {
			t.Errorf("command %q category = %q, want %q", command.Name, command.Category, category)
		}
		delete(expected, command.Name)
	}
	for name := range expected {
		t.Errorf("missing top-level command %q", name)
	}
}

func TestSuggestionsEnabledForEveryCommand(t *testing.T) {
	var visit func([]string, []*cli.Command)
	visit = func(prefix []string, commands []*cli.Command) {
		for _, command := range commands {
			path := append(append([]string(nil), prefix...), command.Name)
			if !command.Suggest {
				t.Errorf("suggestions disabled for zot %s", strings.Join(path, " "))
			}
			visit(path, command.Commands)
		}
	}
	visit(nil, rootCommand().Commands)
}

func TestEveryCommandRendersHelp(t *testing.T) {
	var paths [][]string
	var visit func([]string, []*cli.Command)
	visit = func(prefix []string, commands []*cli.Command) {
		for _, command := range commands {
			path := append(append([]string(nil), prefix...), command.Name)
			paths = append(paths, path)
			visit(path, command.Commands)
		}
	}
	visit(nil, rootCommand().Commands)

	for _, path := range paths {
		path := path
		t.Run(strings.Join(path, "/"), func(t *testing.T) {
			stdout, stderr, err := runCLI("http://unused.invalid", append(path, "--help")...)
			if err != nil {
				t.Fatalf("help: %v\nstderr: %s", err, stderr)
			}
			fullName := "zot " + strings.Join(path, " ")
			if !strings.Contains(stdout, "USAGE:") || !strings.Contains(stdout, fullName) {
				t.Fatalf("help does not identify command path %q:\n%s", fullName, stdout)
			}
		})
	}
}

func TestHelpSuggestsCommandsAndFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "top-level command", args: []string{"serach"}, want: "search"},
		{name: "nested command", args: []string{"item", "crate"}, want: "create"},
		{name: "executable parent command", args: []string{"grant", "revok"}, want: `did you mean "revoke"`},
		{name: "global flag", args: []string{"--jsn", "list"}, want: `Did you mean "--json"?`},
		{name: "leaf flag", args: []string{"search", "--limt", "1", "query"}, want: `Did you mean "--limit"?`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, err := runCLI("http://unused.invalid", test.args...)
			if err == nil {
				t.Fatal("invalid invocation succeeded")
			}
			combined := stdout + stderr + err.Error()
			if !strings.Contains(combined, test.want) {
				t.Fatalf("output missing suggestion %q:\n%s", test.want, combined)
			}
		})
	}
}

func TestMissingInputsIdentifyExactLeaf(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"search"}, want: "zot search"},
		{args: []string{"show"}, want: "zot show"},
		{args: []string{"item", "show"}, want: "zot item show"},     // A1 alias hints its own path
		{args: []string{"item", "search"}, want: "zot item search"}, // A1 alias hints its own path
		{args: []string{"attachment", "show"}, want: "zot attachment show"},
		{args: []string{"attachment", "import"}, want: "zot attachment import --help"},
		{args: []string{"note", "show"}, want: "zot note show"},
		{args: []string{"relation", "list"}, want: "zot relation list"},
		{args: []string{"annotation", "list"}, want: "zot annotation list"},
		{args: []string{"export"}, want: "zot export"},
		{args: []string{"item", "patch"}, want: "zot item patch"},
		{args: []string{"item", "replace"}, want: "zot item replace"},
		{args: []string{"item", "delete"}, want: "zot item delete"},
		{args: []string{"collection", "path"}, want: "zot collection path"},
		{args: []string{"collection", "create"}, want: "zot collection create"},
		{args: []string{"collection", "rename"}, want: "zot collection rename"},
		{args: []string{"collection", "move"}, want: "zot collection move"},
		{args: []string{"collection", "delete"}, want: "zot collection delete"},
		{args: []string{"tag", "add"}, want: "zot tag add"},
		{args: []string{"tag", "remove"}, want: "zot tag remove"},
		{args: []string{"tag", "purge"}, want: "zot tag purge"},
		{args: []string{"tag", "delete"}, want: "zot tag purge"}, // deprecated alias routes to purge
	}
	for _, test := range tests {
		name := strings.Join(test.args, "/")
		t.Run(name, func(t *testing.T) {
			stdout, _, err := runCLI("http://unused.invalid", test.args...)
			if err == nil {
				t.Fatalf("invocation succeeded with output %q", stdout)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want exact leaf guidance %q", err, test.want)
			}
		})
	}
}

func TestHighCostHelpExplainsRouting(t *testing.T) {
	tests := []struct {
		args []string
		want []string
	}{
		{args: []string{"search", "--help"}, want: []string{"--limit 0", "--everything", "full text and notes"}},
		{args: []string{"attachment", "--help"}, want: []string{"attachment show", "attachment import", "local-only"}},
		{args: []string{"note", "--help"}, want: []string{"note show", "rich-text HTML", "do not expose note bodies"}},
		{args: []string{"collection", "--help"}, want: []string{"zot collection path", "zot collections", "local writes"}},
		{args: []string{"item", "--help"}, want: []string{"Create from JSON", "--dry-run", "--yes"}},
		{args: []string{"tag", "--help"}, want: []string{"every item", "--dry-run", "--yes"}},
		{args: []string{"grant", "--help"}, want: []string{"write lease", "interactive", "docs/design/write-authority.md"}},
	}
	for _, test := range tests {
		name := strings.Join(test.args[:len(test.args)-1], "/")
		t.Run(name, func(t *testing.T) {
			stdout, stderr, err := runCLI("http://unused.invalid", test.args...)
			if err != nil {
				t.Fatalf("help: %v\nstderr: %s", err, stderr)
			}
			for _, want := range test.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("help missing %q:\n%s", want, stdout)
				}
			}
		})
	}
}
