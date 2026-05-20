// Copyright 2024 sealos.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const noBaseCommitExitCode = 2

func main() {
	var (
		eventName  = flag.String("event-name", "", "GitHub event name used to determine the comparison base")
		prBaseSHA  = flag.String("pr-base-sha", "", "Pull request base commit SHA")
		beforeSHA  = flag.String("before-sha", "", "Push event before SHA")
		policyPath = flag.String("policy-path", "", "Path to the current LocalPatchPolicy file in the repository")
		outputPath = flag.String("output", "", "Path to write the resolved base LocalPatchPolicy YAML")
	)
	flag.Parse()

	if strings.TrimSpace(*policyPath) == "" || strings.TrimSpace(*outputPath) == "" {
		fmt.Fprintln(os.Stderr, "both --policy-path and --output are required")
		os.Exit(2)
	}

	baseSHA := resolveBaseSHA(*eventName, *prBaseSHA, *beforeSHA)
	if baseSHA == "" {
		fmt.Fprintln(os.Stderr, "no base commit was available for comparison")
		os.Exit(noBaseCommitExitCode)
	}

	data, err := resolvePolicyContent(baseSHA, *policyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve base policy: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outputPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write output: %v\n", err)
		os.Exit(1)
	}
}

func resolveBaseSHA(eventName, prBaseSHA, beforeSHA string) string {
	switch strings.TrimSpace(eventName) {
	case "pull_request":
		return strings.TrimSpace(prBaseSHA)
	default:
		beforeSHA = strings.TrimSpace(beforeSHA)
		if beforeSHA == "" || isZeroSHA(beforeSHA) {
			return ""
		}
		return beforeSHA
	}
}

func resolvePolicyContent(baseSHA, policyPath string) ([]byte, error) {
	if strings.TrimSpace(baseSHA) == "" {
		return nil, errors.New("base SHA cannot be empty")
	}

	ref := fmt.Sprintf("%s:%s", baseSHA, filepathToSlash(policyPath))
	if err := exec.Command("git", "cat-file", "-e", ref).Run(); err == nil {
		data, err := exec.Command("git", "show", ref).Output()
		if err != nil {
			return nil, fmt.Errorf("git show %q: %w", ref, err)
		}
		return data, nil
	}

	data, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, fmt.Errorf("read current policy %q: %w", policyPath, err)
	}
	return data, nil
}

func isZeroSHA(value string) bool {
	return value == "0000000000000000000000000000000000000000"
}

func filepathToSlash(path string) string {
	return strings.ReplaceAll(path, string(os.PathSeparator), "/")
}
