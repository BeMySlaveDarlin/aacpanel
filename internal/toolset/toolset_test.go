package toolset

import (
	"slices"
	"testing"
)

// The server calls a tool by its name and takes the first of the name: a
// second tool of the same name would never be called, and claude would list
// both.
func TestEveryToolHasANameOfItsOwn(t *testing.T) {
	var names []string
	for _, tool := range tools() {
		if tool.Name == "" || slices.Contains(names, tool.Name) {
			t.Errorf("the tool %q is named twice or not at all among %v", tool.Name, names)
		}
		names = append(names, tool.Name)
	}
}

// What the launcher allows is what the tools mark allowed: the plan tool,
// under the name claude gives it and a permission rule knows it by.
func TestTheLauncherIsGivenTheAllowedTools(t *testing.T) {
	if got := Allowed(); !slices.Equal(got, []string{"mcp__aacpanel__plan"}) {
		t.Errorf("allowed %v", got)
	}
}
