// Command webbuild builds the frontend into web/dist.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"aacpanel/internal/webbuild"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("webbuild: ")

	dev := flag.Bool("dev", false, "sourcemaps and readable code instead of minification")
	root := flag.String("root", "", "the repository root (by default it is found by go.mod upwards from the current directory)")
	flag.Parse()

	if *root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
		found, err := webbuild.FindRoot(cwd)
		if err != nil {
			log.Fatal(err)
		}
		*root = found
	}

	files, err := webbuild.Build(webbuild.Options{Root: *root, Dev: *dev})
	if err != nil {
		log.Fatal(err)
	}

	for _, path := range files {
		size := "?"
		if info, err := os.Stat(path); err == nil {
			size = fmt.Sprintf("%d B", info.Size())
		}
		rel, err := filepath.Rel(*root, path)
		if err != nil {
			rel = path
		}
		log.Printf("%s — %s", rel, size)
	}
}
