package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestAPIPathsAvoidTrackerWords(t *testing.T) {
	src, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	routes := regexp.MustCompile(`mux\.Handle(?:Func)?\("([^"]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(routes) < 20 {
		t.Fatalf("%d routes were found in routes.go — the parsing broke, the routes did not run out", len(routes))
	}
	banned := []string{"collect", "track", "beacon", "pixel", "analytics", "telemetry", "metrics", "stats", "ads"}
	for _, route := range routes {
		path := route[1]
		if _, rest, ok := strings.Cut(path, " "); ok {
			path = rest
		}
		for _, segment := range strings.Split(path, "/") {
			for _, word := range banned {
				if segment == word {
					t.Errorf("route %q took the word %q: a blocker will cut it before the network, and the refusal will arrive as a network one", route[1], word)
				}
			}
		}
	}
}
