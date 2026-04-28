package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/labring/sealos/pkg/distribution/packageformat"
)

const (
	formatEnv      = "env"
	formatPathList = "path-list"
)

func main() {
	var packageDir string
	var format string

	flag.StringVar(&packageDir, "package-dir", "", "package directory to inspect")
	flag.StringVar(&format, "format", formatEnv, "output format: env or path-list")
	flag.Parse()

	if packageDir == "" {
		exitf("package directory cannot be empty")
	}

	absDir, err := pathAbs(packageDir)
	if err != nil {
		exitf("resolve package directory %q: %v", packageDir, err)
	}

	pkg, err := packageformat.LoadDir(absDir)
	if err != nil {
		exitf("%v", err)
	}

	switch format {
	case formatEnv:
		printEnv(absDir, pkg)
	case formatPathList:
		printPathList(pkg)
	default:
		exitf("unsupported format %q", format)
	}
}

func printEnv(packageDir string, pkg *packageformat.ComponentPackage) {
	fmt.Printf("package_root=%s\n", shellQuote(packageDir))
	fmt.Printf("PACKAGE_NAME=%s\n", shellQuote(pkg.Metadata.Name))
	fmt.Printf("PACKAGE_COMPONENT=%s\n", shellQuote(pkg.Spec.Component))
	fmt.Printf("PACKAGE_VERSION=%s\n", shellQuote(pkg.Spec.Version))
	fmt.Printf("PACKAGE_CLASS=%s\n", shellQuote(string(pkg.Spec.Class)))
}

func printPathList(pkg *packageformat.ComponentPackage) {
	paths := []string{packageformat.ManifestFileName}
	for _, content := range pkg.Spec.Contents {
		paths = append(paths, mustClean(content.Path))
	}
	for _, hook := range pkg.Spec.Hooks {
		paths = append(paths, mustClean(hook.Path))
	}

	for _, relPath := range compactPaths(paths) {
		fmt.Println(relPath)
	}
}

func compactPaths(paths []string) []string {
	cleaned := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, relPath := range paths {
		if _, ok := seen[relPath]; ok {
			continue
		}
		seen[relPath] = struct{}{}
		cleaned = append(cleaned, relPath)
	}

	slices.SortFunc(cleaned, func(a, b string) int {
		aDepth := strings.Count(a, "/")
		bDepth := strings.Count(b, "/")
		switch {
		case aDepth < bDepth:
			return -1
		case aDepth > bDepth:
			return 1
		case a < b:
			return -1
		case a > b:
			return 1
		default:
			return 0
		}
	})

	kept := make([]string, 0, len(cleaned))
	for _, relPath := range cleaned {
		skip := false
		for _, parent := range kept {
			if relPath == parent || strings.HasPrefix(relPath, parent+"/") {
				skip = true
				break
			}
		}
		if !skip {
			kept = append(kept, relPath)
		}
	}
	return kept
}

func mustClean(relPath string) string {
	cleaned := path.Clean(relPath)
	switch {
	case cleaned == ".":
		exitf("invalid relative path %q", relPath)
	case strings.HasPrefix(cleaned, "/"):
		exitf("path %q must be relative", relPath)
	case strings.HasPrefix(cleaned, "../"):
		exitf("path %q escapes package root", relPath)
	default:
		return cleaned
	}

	return ""
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func pathAbs(dir string) (string, error) {
	return filepath.Abs(dir)
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
