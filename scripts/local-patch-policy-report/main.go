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
	"flag"
	"fmt"
	"os"

	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/ownership"
	"github.com/labring/sealos/pkg/distribution/policyreport"
)

func main() {
	var (
		oldPolicyPath = flag.String("old-policy", "", "Path to the previous LocalPatchPolicy YAML")
		newPolicyPath = flag.String("new-policy", "", "Path to the candidate LocalPatchPolicy YAML")
		localRepoRoot = flag.String("local-repo", "", "Optional local repo root used to evaluate existing patch compatibility")
	)
	flag.Parse()

	if *oldPolicyPath == "" || *newPolicyPath == "" {
		fmt.Fprintln(os.Stderr, "both --old-policy and --new-policy are required")
		os.Exit(2)
	}

	oldDoc, err := ownership.LoadLocalPatchPolicyFile(*oldPolicyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load old policy: %v\n", err)
		os.Exit(1)
	}
	newDoc, err := ownership.LoadLocalPatchPolicyFile(*newPolicyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load new policy: %v\n", err)
		os.Exit(1)
	}

	var repo *localrepo.Repo
	if *localRepoRoot != "" {
		repo, err = localrepo.Load(*localRepoRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load local repo: %v\n", err)
			os.Exit(1)
		}
	}

	report, err := policyreport.Build(oldDoc, newDoc, repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build report: %v\n", err)
		os.Exit(1)
	}

	data, err := yaml.Marshal(report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal report: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		fmt.Fprintf(os.Stderr, "write report: %v\n", err)
		os.Exit(1)
	}
}
