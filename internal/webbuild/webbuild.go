// Package webbuild builds the frontend.
package webbuild

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// SrcDir and the paths next to it are the locations inside the repository.
const (
	SrcDir    = "web/src"
	VendorDir = "web/vendor"
	DistDir   = "web/dist"
	IconsDir  = "web/static/icons"
	FontsDir  = "web/static/fonts"

	appEntry   = "main.js"
	appBundle  = "bundle"
	termEntry  = "term.js"
	termBundle = "term"
	swEntry    = "sw.js"
	swBundle   = "sw"
)

var shellFiles = []string{"web/app.html", "web/manifest.webmanifest"}

var vendorAlias = map[string]string{
	"preact":       "preact.mjs",
	"preact/hooks": "preact-hooks.mjs",
	"htm":          "htm.mjs",
	"uplot":        "uplot.mjs",
	"xterm":        "xterm.mjs",
	"xterm-fit":    "xterm-fit.mjs",
}

// Aliases is the import map of the vendored libraries for esbuild.
func Aliases(root string) (map[string]string, error) {
	alias := make(map[string]string, len(vendorAlias))
	for imp, file := range vendorAlias {
		path := filepath.Join(root, VendorDir, file)
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("vendored library %q: %w — put the file into %s or drop the import", imp, err, VendorDir)
		}
		alias[imp] = path
	}
	return alias, nil
}

// Options says what to build and how.
type Options struct {
	Root string
	Dev  bool
}

// Build compiles the frontend sources and returns the paths of the written files.
func Build(o Options) ([]string, error) {
	if o.Root == "" {
		return nil, errors.New("the repository root is not set: the build looks for web/src, web/vendor and web/dist inside it")
	}
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return nil, fmt.Errorf("repository root: %w", err)
	}

	alias, err := Aliases(root)
	if err != nil {
		return nil, err
	}

	dist := filepath.Join(root, DistDir)
	if err := os.MkdirAll(dist, 0o755); err != nil {
		return nil, fmt.Errorf("directory %s: %w", DistDir, err)
	}
	if err := clean(dist); err != nil {
		return nil, err
	}

	credits, err := banner(filepath.Join(root, VendorDir))
	if err != nil {
		return nil, err
	}

	written, err := run(o, root, alias, api.BuildOptions{
		EntryPoints: []string{filepath.Join(root, SrcDir, appEntry)},
		Outfile:     filepath.Join(dist, appBundle+".js"),
		Format:      api.FormatESModule,
		Banner:      map[string]string{"js": credits, "css": credits},
	})
	if err != nil {
		return nil, err
	}

	term, err := run(o, root, alias, api.BuildOptions{
		EntryPoints: []string{filepath.Join(root, SrcDir, termEntry)},
		Outfile:     filepath.Join(dist, termBundle+".js"),
		Format:      api.FormatESModule,
		Banner:      map[string]string{"js": credits},
	})
	if err != nil {
		return nil, err
	}
	written = append(written, term...)

	assets, err := shellAssets(root)
	if err != nil {
		return nil, err
	}
	version, err := buildVersion(root, written, assets)
	if err != nil {
		return nil, err
	}
	list, err := json.Marshal(assets)
	if err != nil {
		return nil, fmt.Errorf("app shell list: %w", err)
	}

	sw, err := run(o, root, alias, api.BuildOptions{
		EntryPoints: []string{filepath.Join(root, SrcDir, swEntry)},
		Outfile:     filepath.Join(dist, swBundle+".js"),
		Format:      api.FormatIIFE,
		Define: map[string]string{
			"__VERSION__": strconv.Quote(version),
			"__ASSETS__":  string(list),
		},
	})
	if err != nil {
		return nil, err
	}

	return append(written, sw...), nil
}

func run(o Options, root string, alias map[string]string, opts api.BuildOptions) ([]string, error) {
	entry := opts.EntryPoints[0]
	if _, err := os.Stat(entry); err != nil {
		return nil, fmt.Errorf("entry point: %w", err)
	}

	opts.AbsWorkingDir = root
	opts.Bundle = true
	opts.Write = true
	opts.Platform = api.PlatformBrowser
	opts.Target = api.ES2022
	opts.Alias = alias
	opts.ResolveExtensions = []string{".js", ".mjs", ".css"}
	opts.Loader = map[string]api.Loader{
		".css": api.LoaderCSS,
		".svg": api.LoaderDataURL,
	}
	opts.External = append(opts.External, "/static/*")
	opts.External = append(opts.External, "/dist/*")
	if o.Dev {
		opts.Sourcemap = api.SourceMapLinked
	}
	opts.MinifyWhitespace = !o.Dev
	opts.MinifyIdentifiers = !o.Dev
	opts.MinifySyntax = !o.Dev
	opts.LegalComments = api.LegalCommentsEndOfFile
	opts.LogLevel = api.LogLevelSilent

	result := api.Build(opts)

	for _, line := range api.FormatMessages(result.Warnings, api.FormatMessagesOptions{Kind: api.WarningMessage}) {
		fmt.Fprint(os.Stderr, line)
	}
	if len(result.Errors) > 0 {
		lines := api.FormatMessages(result.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage})
		return nil, fmt.Errorf("esbuild:\n%s", strings.TrimRight(strings.Join(lines, ""), "\n"))
	}

	written := make([]string, 0, len(result.OutputFiles))
	for _, f := range result.OutputFiles {
		written = append(written, f.Path)
	}
	return written, nil
}

func shellAssets(root string) ([]string, error) {
	assets := []string{
		"/dist/" + appBundle + ".js",
		"/dist/" + appBundle + ".css",
		"/manifest.webmanifest",
	}

	entries, err := os.ReadDir(filepath.Join(root, IconsDir))
	if err != nil {
		return nil, fmt.Errorf("icons: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".png", ".svg":
			assets = append(assets, path.Join("/static/icons", e.Name()))
		}
	}

	fonts, err := os.ReadDir(filepath.Join(root, FontsDir))
	if err != nil {
		return nil, fmt.Errorf("fonts: %w", err)
	}
	for _, e := range fonts {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".woff2") {
			assets = append(assets, path.Join("/static/fonts", e.Name()))
		}
	}
	sort.Strings(assets)
	return assets, nil
}

func buildVersion(root string, built, assets []string) (string, error) {
	sum := sha256.New()

	files := append([]string{}, built...)
	for _, rel := range shellFiles {
		files = append(files, filepath.Join(root, filepath.FromSlash(rel)))
	}
	sort.Strings(files)

	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return "", fmt.Errorf("build version: %w", err)
		}
		fmt.Fprintf(sum, "%s\n", filepath.Base(f))
		sum.Write(raw)
	}
	for _, a := range assets {
		fmt.Fprintf(sum, "%s\n", a)
	}

	return hex.EncodeToString(sum.Sum(nil))[:12], nil
}

func banner(vendor string) (string, error) {
	entries, err := os.ReadDir(vendor)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", VendorDir, err)
	}

	var libs []string
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(vendor, e.Name()))
		if err != nil {
			return "", fmt.Errorf("reading %s/%s: %w", VendorDir, e.Name(), err)
		}
		first, _, _ := strings.Cut(string(raw), "\n")
		first = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(first), "/*"))
		if first == "" || seen[first] {
			continue
		}
		seen[first] = true
		libs = append(libs, first)
	}
	if len(libs) == 0 {
		return "", fmt.Errorf("no vendored libraries in %s: the bundle needs them, fetch them before building", VendorDir)
	}

	var b strings.Builder
	b.WriteString("/*! Bundled by esbuild from the vendored libraries:\n")
	for _, lib := range libs {
		b.WriteString(" * " + lib + "\n")
	}
	b.WriteString(" * Sources, origins and sha256 are in " + VendorDir + "/.\n */")
	return b.String(), nil
}

func clean(dist string) error {
	entries, err := os.ReadDir(dist)
	if err != nil {
		return fmt.Errorf("reading %s: %w", DistDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), appBundle+".") && !strings.HasPrefix(e.Name(), swBundle+".") {
			continue
		}
		if err := os.Remove(filepath.Join(dist, e.Name())); err != nil {
			return fmt.Errorf("cleaning %s: %w", DistDir, err)
		}
	}
	return nil
}

// FindRoot looks for the repository root, the directory with go.mod, upwards from start.
func FindRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s — run the build from inside the repository", start)
		}
		dir = parent
	}
}
