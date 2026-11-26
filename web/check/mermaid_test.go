package check

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestMermaidBlocksRenderLazilyAndKeepTheirSource(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desktop", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			var got struct {
				Lazy, Diagram, DiagramHidden, Themed bool
				Initial, Copied, Code                string
				Loads                                int
				Bad                                  struct {
					Code, Error string
					Shown       bool
					Errors      int
				}
				Safe struct {
					Attacks              int
					Unsafe, Strict, HTML bool
				}
			}
			screen.run(t, "mermaid.html", &got)
			const source = "flowchart TD\nA[Start] --> B[Done]"
			if !got.Lazy || got.Loads != 1 || !got.Diagram || !got.Themed {
				t.Errorf("diagram lifecycle: lazy=%v, loads=%d, rendered=%v, themed=%v", got.Lazy, got.Loads, got.Diagram, got.Themed)
			}
			if got.Initial != source || got.Copied != source || got.Code != source || !got.DiagramHidden {
				t.Errorf("source, copy and mode switch: %+v", got)
			}
			if got.Bad.Code != "flowchart TD\nA[broken" || !got.Bad.Shown || got.Bad.Errors != 1 || !strings.Contains(got.Bad.Error, "Parse error") || strings.Contains(got.Bad.Error, "\n    at ") {
				t.Errorf("a parse error lost its source or error line: %+v", got.Bad)
			}
			if got.Safe.Attacks != 0 || got.Safe.Unsafe || !got.Safe.Strict || got.Safe.HTML {
				t.Errorf("a diagram escaped the strict, text-only renderer: %+v", got.Safe)
			}
		})
	}
}

func TestMermaidStaysOutsideTheShellPrecacheAndBundle(t *testing.T) {
	vendor, err := os.ReadFile(webPath("vendor/mermaid.min.js"))
	if err != nil {
		t.Fatal(err)
	}
	asset, err := os.ReadFile(webPath("dist/mermaid.js"))
	if err != nil || !bytes.Equal(vendor, asset) {
		t.Fatalf("the on-demand diagram engine was not shipped: %v", err)
	}
	for _, name := range []string{"dist/bundle.js", "dist/sw.js"} {
		raw, err := os.ReadFile(webPath(name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("__esbuild_esm_mermaid_nm")) {
			t.Errorf("%s includes the diagram engine eagerly", name)
		}
		if name == "dist/sw.js" && bytes.Contains(raw, []byte("/dist/mermaid.js")) {
			t.Error("the diagram engine is downloaded by the shell precache")
		}
	}
}
