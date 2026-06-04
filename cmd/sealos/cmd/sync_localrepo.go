// Copyright © 2026 sealos.
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

package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/ownership"
	"github.com/labring/sealos/pkg/distribution/packageformat"
)

type syncLocalRepoInitOutput struct {
	LocalRepo             string                        `json:"localRepo" yaml:"localRepo"`
	LocalRepoMetadata     string                        `json:"localRepoMetadataPath,omitempty" yaml:"localRepoMetadataPath,omitempty"`
	LocalRepoRevision     string                        `json:"localRepoRevision,omitempty" yaml:"localRepoRevision,omitempty"`
	LocalRepoRevisionPath string                        `json:"localRepoRevisionPath,omitempty" yaml:"localRepoRevisionPath,omitempty"`
	LocalInputRevision    string                        `json:"localInputRevision,omitempty" yaml:"localInputRevision,omitempty"`
	BOMPath               string                        `json:"bomPath" yaml:"bomPath"`
	BOMDigest             string                        `json:"bomDigest,omitempty" yaml:"bomDigest,omitempty"`
	ReleaseChannelPath    string                        `json:"releaseChannelPath,omitempty" yaml:"releaseChannelPath,omitempty"`
	ReleaseSource         string                        `json:"releaseSource,omitempty" yaml:"releaseSource,omitempty"`
	ReleaseLine           string                        `json:"releaseLine,omitempty" yaml:"releaseLine,omitempty"`
	Channel               string                        `json:"channel,omitempty" yaml:"channel,omitempty"`
	BOMName               string                        `json:"bomName" yaml:"bomName"`
	Revision              string                        `json:"revision" yaml:"revision"`
	Components            int                           `json:"components" yaml:"components"`
	Summary               syncLocalRepoInitSummary      `json:"summary" yaml:"summary"`
	Inputs                []syncLocalRepoInitInput      `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	SecretHints           []syncLocalRepoInitSecretHint `json:"secretHints,omitempty" yaml:"secretHints,omitempty"`
	WrittenFiles          []string                      `json:"writtenFiles,omitempty" yaml:"writtenFiles,omitempty"`
	SkippedFiles          []string                      `json:"skippedFiles,omitempty" yaml:"skippedFiles,omitempty"`
	NextSteps             []string                      `json:"nextSteps" yaml:"nextSteps"`
}

type syncLocalRepoInitSummary struct {
	RequiredInputs int `json:"requiredInputs" yaml:"requiredInputs"`
	OptionalInputs int `json:"optionalInputs" yaml:"optionalInputs"`
	SecretHints    int `json:"secretHints" yaml:"secretHints"`
	WrittenFiles   int `json:"writtenFiles" yaml:"writtenFiles"`
	SkippedFiles   int `json:"skippedFiles" yaml:"skippedFiles"`
}

type syncLocalRepoInitInput struct {
	Component string `json:"component" yaml:"component"`
	Name      string `json:"name" yaml:"name"`
	Type      string `json:"type" yaml:"type"`
	Format    string `json:"format,omitempty" yaml:"format,omitempty"`
	Required  bool   `json:"required" yaml:"required"`
	Path      string `json:"path" yaml:"path"`
}

type syncLocalRepoInitSecretHint struct {
	Component string `json:"component,omitempty" yaml:"component,omitempty"`
	Input     string `json:"input,omitempty" yaml:"input,omitempty"`
	Path      string `json:"path" yaml:"path"`
	Reason    string `json:"reason" yaml:"reason"`
}

type syncLocalRepoInitOptions struct {
	ClusterName        string
	BOMPath            string
	ReleaseChannelPath string
	ReleaseSource      string
	ReleaseLine        string
	Channel            string
	OutputDir          string
	PackageSources     []string
	Overwrite          bool
	CreatedAt          time.Time
}

type syncLocalRepoDoctorOptions struct {
	ClusterName        string
	BOMPath            string
	ReleaseChannelPath string
	ReleaseSource      string
	ReleaseLine        string
	Channel            string
	LocalRepoPath      string
	PackageSources     []string
}

type syncLocalRepoTargetMetadata struct {
	Cluster          string
	DistributionLine string
	Channel          string
	BOMName          string
	BOMRevision      string
	BOMDigest        string
}

type syncLocalRepoInitWriter struct {
	root      string
	overwrite bool
	written   []string
	skipped   []string
}

func newSyncLocalRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "local-repo",
		Short: "Initialize and inspect cluster-local sync repository content",
		Args:  cobra.NoArgs,
	}
	initCmd := newSyncLocalRepoInitCmd()
	addSyncRuntimeRootFlag(initCmd)
	cmd.AddCommand(initCmd)
	doctorCmd := newSyncLocalRepoDoctorCmd()
	addSyncRuntimeRootFlag(doctorCmd)
	cmd.AddCommand(doctorCmd)
	return cmd
}

func runSyncLocalRepoDoctor(opts syncLocalRepoDoctorOptions) syncLocalRepoDoctorOutput {
	acc := syncLocalRepoDoctorAccumulator{}
	acc.out.BOMPath = strings.TrimSpace(opts.BOMPath)
	acc.out.ReleaseChannelPath = strings.TrimSpace(opts.ReleaseChannelPath)
	acc.out.ReleaseSource = strings.TrimSpace(opts.ReleaseSource)
	acc.out.ReleaseLine = strings.TrimSpace(opts.ReleaseLine)
	acc.out.Channel = strings.TrimSpace(opts.Channel)
	acc.out.LocalRepo = strings.TrimSpace(opts.LocalRepoPath)

	target, resolved, err := resolveSyncLocalRepoBOMPackages(opts.ClusterName, syncTargetOptions{
		BOMPath:            opts.BOMPath,
		ReleaseChannelPath: opts.ReleaseChannelPath,
		ReleaseSource:      opts.ReleaseSource,
		ReleaseLine:        opts.ReleaseLine,
		Channel:            opts.Channel,
	}, opts.PackageSources)
	if err != nil {
		acc.error("packageContractInvalid", "", "", opts.BOMPath, err.Error(), "Fix the BOM or package-source flags, then rerun local-repo doctor.")
		return acc.finalize()
	}
	doc := target.BOM
	acc.out.BOMPath = target.BOMPath
	if strings.TrimSpace(target.ReleaseChannelPath) != "" {
		acc.out.ReleaseChannelPath = strings.TrimSpace(target.ReleaseChannelPath)
	}
	if strings.TrimSpace(target.ReleaseSource) != "" {
		acc.out.ReleaseSource = strings.TrimSpace(target.ReleaseSource)
	}
	if strings.TrimSpace(target.ReleaseLine) != "" {
		acc.out.ReleaseLine = strings.TrimSpace(target.ReleaseLine)
	}
	if target.Channel != "" {
		acc.out.Channel = string(target.Channel)
	}
	acc.out.BOMName = doc.Metadata.Name
	acc.out.Revision = doc.Spec.Revision
	acc.out.BOMDigest = syncLocalRepoTargetBOMDigest(target)
	acc.out.Summary.Components = doc.PackageCount()

	repoRoot, err := filepath.Abs(opts.LocalRepoPath)
	if err != nil {
		acc.error("localRepoInvalid", "", "", opts.LocalRepoPath, fmt.Sprintf("resolve local repo path: %v", err), "Pass a valid --local-repo path.")
		return acc.finalize()
	}
	acc.out.LocalRepo = repoRoot
	repo, err := localrepo.Load(repoRoot)
	if err != nil {
		acc.error("localRepoInvalid", "", "", repoRoot, err.Error(), "Run sync local-repo init or fix the local repo layout.")
		return acc.finalize()
	}
	acc.out.LocalRepoMetadata = filepath.Join(repo.Root, localrepo.RepoFileName)
	acc.out.LocalRepoRevisionPath = filepath.Join(repo.Root, localrepo.RevisionsDirName, localrepo.CurrentRevisionFileName)
	acc.out.LocalRepoRevision = repo.Revision
	acc.out.LocalInputRevision = repo.InputRevision
	acc.out.Summary.Resources = len(repo.Resources())
	acc.out.Summary.Patches = syncLocalRepoDoctorPatchCount(repo, doc)
	acc.checkLocalRepoSchemaWarnings(repo)
	acc.checkLocalRepoSchema(repo, syncLocalRepoTargetMetadataForTarget(opts.ClusterName, target))

	inputContracts := collectSyncLocalRepoInitInputs(doc, resolved)
	acc.out.Summary.Inputs = len(inputContracts)
	for _, input := range inputContracts {
		if input.Required {
			acc.out.Summary.RequiredInputs++
		}
		acc.checkInput(repo, input)
	}
	acc.checkLocalRepoPolicy(repo, repoRoot)
	acc.checkResources(repo)
	acc.checkStaleComponentDirs(doc, repoRoot)
	return acc.finalize()
}

func resolveSyncLocalRepoBOMPackages(clusterName string, targetOpts syncTargetOptions, packageSources []string) (*syncResolvedTarget, map[string]*packageformat.ComponentPackage, error) {
	target, err := resolveSyncTarget(targetOpts)
	if err != nil {
		return nil, nil, err
	}
	doc := target.BOM
	localRoots, localPackages, err := resolveSyncPackageSources(doc, packageSources)
	if err != nil {
		return nil, nil, err
	}
	var fallbackLoader packageformat.Loader
	if len(localRoots) < doc.PackageCount() {
		fallbackLoader, _, err = newSyncCachedArtifactResolver(clusterName)
		if err != nil {
			return nil, nil, err
		}
	}
	resolved, err := doc.ResolveComponentPackages(syncPackageLoader{
		local:    localPackages,
		fallback: fallbackLoader,
	})
	if err != nil {
		return nil, nil, err
	}
	return target, resolved, nil
}

func (a *syncLocalRepoDoctorAccumulator) issue(severity syncLocalRepoDoctorSeverity, code, component, input, path, message, suggestedFix string) {
	a.out.Issues = append(a.out.Issues, syncLocalRepoDoctorIssue{
		Severity:     severity,
		Code:         strings.TrimSpace(code),
		Component:    strings.TrimSpace(component),
		Input:        strings.TrimSpace(input),
		Path:         strings.TrimSpace(path),
		Message:      strings.TrimSpace(message),
		SuggestedFix: strings.TrimSpace(suggestedFix),
	})
	switch severity {
	case syncLocalRepoDoctorSeverityError:
		a.out.Summary.Errors++
	case syncLocalRepoDoctorSeverityWarning:
		a.out.Summary.Warnings++
	}
}

func (a *syncLocalRepoDoctorAccumulator) error(code, component, input, path, message, suggestedFix string) {
	a.issue(syncLocalRepoDoctorSeverityError, code, component, input, path, message, suggestedFix)
}

func (a *syncLocalRepoDoctorAccumulator) warning(code, component, input, path, message, suggestedFix string) {
	a.issue(syncLocalRepoDoctorSeverityWarning, code, component, input, path, message, suggestedFix)
}

func (a *syncLocalRepoDoctorAccumulator) finalize() syncLocalRepoDoctorOutput {
	sort.Slice(a.out.Issues, func(i, j int) bool {
		left := a.out.Issues[i]
		right := a.out.Issues[j]
		if left.Severity != right.Severity {
			return left.Severity < right.Severity
		}
		if left.Component != right.Component {
			return left.Component < right.Component
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.Input != right.Input {
			return left.Input < right.Input
		}
		return left.Path < right.Path
	})
	a.out.Passed = a.out.Summary.Errors == 0
	a.out.Suggested = syncLocalRepoDoctorSuggested(a.out)
	return a.out
}

func (a *syncLocalRepoDoctorAccumulator) checkInput(repo *localrepo.Repo, input syncLocalRepoInitInput) {
	if repo == nil {
		return
	}
	binding, ok := repo.BindingFor(input.Component, packageformat.Input{Name: input.Name})
	if !ok {
		if input.Required {
			expectedPath := filepath.Join(repo.Root, filepath.FromSlash(input.Path))
			a.error(
				"requiredInputMissing",
				input.Component,
				input.Name,
				expectedPath,
				fmt.Sprintf("required input %q for component %q is not initialized in the local repo", input.Name, input.Component),
				fmt.Sprintf("Create %s or rerun sealos sync local-repo init for this BOM.", syncShellQuote(expectedPath)),
			)
		}
		return
	}

	data, err := os.ReadFile(binding)
	if err != nil {
		a.error(
			"inputInvalid",
			input.Component,
			input.Name,
			binding,
			fmt.Sprintf("read input file %q: %v", binding, err),
			"Fix the input file so local-repo doctor can read it.",
		)
		return
	}
	if syncLocalRepoDoctorIsGeneratedPlaceholder(input, data) {
		a.out.Summary.PlaceholderHits++
		message := fmt.Sprintf("input %q for component %q still contains the generated local-repo init placeholder", input.Name, input.Component)
		suggestedFix := fmt.Sprintf("Replace the generated placeholder in %s with the real cluster-local value.", syncShellQuote(binding))
		if input.Required {
			a.error("inputPlaceholder", input.Component, input.Name, binding, message, suggestedFix)
		} else {
			a.warning("inputPlaceholder", input.Component, input.Name, binding, message, suggestedFix)
		}
	}
	if !syncLocalRepoDoctorInputLooksSecret(input, binding) {
		return
	}
	info, err := os.Stat(binding)
	if err != nil {
		a.error(
			"inputInvalid",
			input.Component,
			input.Name,
			binding,
			fmt.Sprintf("stat secret-like input file %q: %v", binding, err),
			"Fix the input file so local-repo doctor can inspect its permissions.",
		)
		return
	}
	if !info.IsDir() && info.Mode().Perm()&0o077 != 0 {
		a.error(
			"secretInputModeTooOpen",
			input.Component,
			input.Name,
			binding,
			fmt.Sprintf("secret-like input %q is readable by group or other users; current mode is %s", binding, info.Mode().Perm()),
			fmt.Sprintf("Run chmod 0600 %s for secret-like local repo inputs.", syncShellQuote(binding)),
		)
	}
}

func (a *syncLocalRepoDoctorAccumulator) checkLocalRepoPolicy(repo *localrepo.Repo, repoRoot string) {
	if repo == nil {
		return
	}
	policyDoc := repo.LocalPatchPolicy()
	policyPath := filepath.Join(repoRoot, localrepo.PolicyDirName, ownership.LocalPatchPolicyFileName)
	if policyDoc == nil {
		a.warning(
			"localPatchPolicyMissing",
			"",
			"",
			policyPath,
			"local repo does not define policy/local-patch-policy.yaml; render will fall back to the built-in default policy",
			"Create policy/local-patch-policy.yaml with sealos sync local-repo init or an explicitly reviewed LocalPatchPolicy.",
		)
		return
	}

	compatibility, err := localrepo.EvaluatePatchCompatibility(repo, policyDoc.Spec)
	if err != nil {
		a.error(
			"localPatchPolicyInvalid",
			"",
			"",
			repo.LocalPatchPolicyRelativePath(),
			err.Error(),
			"Fix policy/local-patch-policy.yaml or the local patch files it evaluates.",
		)
		return
	}
	for _, issue := range compatibility.InvalidPatches {
		a.error(
			"localPatchPolicyViolation",
			issue.Component,
			"",
			filepath.ToSlash(filepath.Join(localrepo.PatchesDirName, issue.Component, issue.RelativePath)),
			issue.Reason,
			"Adjust the local patch or update policy/local-patch-policy.yaml through the local patch policy review flow.",
		)
	}
}

func (a *syncLocalRepoDoctorAccumulator) checkLocalRepoSchema(repo *localrepo.Repo, target syncLocalRepoTargetMetadata) {
	if repo == nil {
		return
	}
	if repo.Metadata == nil {
		a.warning(
			"localRepoMetadataMissing",
			"",
			"",
			filepath.Join(repo.Root, localrepo.RepoFileName),
			"local repo metadata schema file is missing",
			"Run sealos sync local-repo init --overwrite with the same target to write repo.yaml.",
		)
	} else {
		a.checkLocalRepoMetadata(repo.Metadata, target, repo.Root)
	}
	if repo.Current == nil {
		a.warning(
			"localRepoRevisionMissing",
			"",
			"",
			filepath.Join(repo.Root, localrepo.RevisionsDirName, localrepo.CurrentRevisionFileName),
			"local repo current revision schema file is missing",
			"Run sealos sync local-repo init --overwrite with the same target to write revisions/current.yaml.",
		)
		return
	}
	a.checkLocalRepoRevision(repo.Current, target, repo)
}

func (a *syncLocalRepoDoctorAccumulator) checkLocalRepoSchemaWarnings(repo *localrepo.Repo) {
	if repo == nil {
		return
	}
	for _, warning := range repo.SchemaWarnings {
		a.warning(
			"localRepoSchemaInvalid",
			"",
			"",
			repo.Root,
			warning,
			"Run sealos sync local-repo init --overwrite with the same target to refresh repo.yaml and revisions/current.yaml.",
		)
	}
}

func (a *syncLocalRepoDoctorAccumulator) checkLocalRepoMetadata(doc *localrepo.LocalRepoDocument, target syncLocalRepoTargetMetadata, repoRoot string) {
	if doc == nil {
		return
	}
	if got, want := strings.TrimSpace(doc.Spec.Cluster), strings.TrimSpace(target.Cluster); got != "" && want != "" && got != want {
		a.warning(
			"localRepoClusterMismatch",
			"",
			"",
			filepath.Join(repoRoot, localrepo.RepoFileName),
			fmt.Sprintf("local repo metadata cluster %q does not match selected cluster %q", got, want),
			"Reinitialize repo.yaml with the selected cluster or inspect the target selection.",
		)
	}
	if got, want := strings.TrimSpace(doc.Spec.DistributionLine), strings.TrimSpace(target.DistributionLine); got != "" && want != "" && got != want {
		a.warning(
			"localRepoDistributionLineMismatch",
			"",
			"",
			filepath.Join(repoRoot, localrepo.RepoFileName),
			fmt.Sprintf("local repo metadata distributionLine %q does not match selected line %q", got, want),
			"Reinitialize repo.yaml with the selected release line or inspect the target selection.",
		)
	}
}

func (a *syncLocalRepoDoctorAccumulator) checkLocalRepoRevision(doc *localrepo.LocalRepoRevisionDocument, target syncLocalRepoTargetMetadata, repo *localrepo.Repo) {
	if doc == nil || repo == nil {
		return
	}
	revisionPath := filepath.Join(repo.Root, localrepo.RevisionsDirName, localrepo.CurrentRevisionFileName)
	if got, want := strings.TrimSpace(doc.Spec.Cluster), strings.TrimSpace(target.Cluster); got != "" && want != "" && got != want {
		a.warning("localRepoRevisionClusterMismatch", "", "", revisionPath, fmt.Sprintf("local repo revision cluster %q does not match selected cluster %q", got, want), "Refresh revisions/current.yaml with the selected cluster before using it for audit.")
	}
	if got, want := strings.TrimSpace(doc.Spec.DistributionLine), strings.TrimSpace(target.DistributionLine); got != "" && want != "" && got != want {
		a.warning("localRepoRevisionLineMismatch", "", "", revisionPath, fmt.Sprintf("local repo revision distributionLine %q does not match selected line %q", got, want), "Refresh revisions/current.yaml with the selected release line before using it for audit.")
	}
	if got, want := strings.TrimSpace(doc.Spec.BOM.Name), strings.TrimSpace(target.BOMName); got != "" && want != "" && got != want {
		a.warning("localRepoRevisionBOMMismatch", "", "", revisionPath, fmt.Sprintf("local repo revision BOM %q does not match selected BOM %q", got, want), "Refresh revisions/current.yaml with the selected BOM before using it for audit.")
	}
	if got, want := strings.TrimSpace(doc.Spec.BOM.Revision), strings.TrimSpace(target.BOMRevision); got != "" && want != "" && got != want {
		a.warning("localRepoRevisionBOMRevisionMismatch", "", "", revisionPath, fmt.Sprintf("local repo revision BOM revision %q does not match selected revision %q", got, want), "Refresh revisions/current.yaml with the selected BOM before using it for audit.")
	}
}

func (a *syncLocalRepoDoctorAccumulator) checkResources(repo *localrepo.Repo) {
	if repo == nil {
		return
	}
	for _, resource := range repo.Resources() {
		secretLike := syncLocalRepoDoctorPathLooksSecret(resource.RelativePath)
		if !syncLocalRepoDoctorIsManifestPath(resource.RelativePath) {
			a.warning(
				"resourceNonManifestFile",
				"",
				"",
				resource.Path,
				fmt.Sprintf("local repo resource %q is not a Kubernetes manifest file", resource.RelativePath),
				"Keep resources/ limited to .yaml, .yml, or .json Kubernetes manifests.",
			)
			a.checkSecretResourceMode(resource, secretLike)
			continue
		}

		kind := syncValidateManifestKind(resource.Path)
		if kind == "" {
			a.error(
				"resourceManifestInvalid",
				"",
				"",
				resource.Path,
				fmt.Sprintf("local repo resource %q is not a readable Kubernetes manifest with a kind", resource.RelativePath),
				"Fix or remove the resource manifest before render/apply.",
			)
			a.checkSecretResourceMode(resource, secretLike)
			continue
		}
		if secretLike && !syncLocalRepoDoctorAllowedSecretResourceKind(kind) {
			a.error(
				"secretResourceKindInvalid",
				"",
				"",
				resource.Path,
				fmt.Sprintf("secret-like resource path %q uses kind %q", resource.RelativePath, kind),
				"Use kind Secret, ExternalSecret, ClusterExternalSecret, or SealedSecret for files under secret-like resource paths.",
			)
		}
		a.checkSecretResourceMode(resource, secretLike || syncLocalRepoDoctorAllowedSecretResourceKind(kind))
	}
}

func (a *syncLocalRepoDoctorAccumulator) checkSecretResourceMode(resource localrepo.Resource, secretLike bool) {
	if !secretLike {
		return
	}
	info, err := os.Stat(resource.Path)
	if err != nil {
		a.error(
			"resourceInvalid",
			"",
			"",
			resource.Path,
			fmt.Sprintf("stat secret-like resource file %q: %v", resource.RelativePath, err),
			"Fix the resource file so local-repo doctor can inspect its permissions.",
		)
		return
	}
	if !info.IsDir() && info.Mode().Perm()&0o077 != 0 {
		a.error(
			"secretResourceModeTooOpen",
			"",
			"",
			resource.Path,
			fmt.Sprintf("secret-like resource %q is readable by group or other users; current mode is %s", resource.RelativePath, info.Mode().Perm()),
			fmt.Sprintf("Run chmod 0600 %s for secret-like local repo resources.", syncShellQuote(resource.Path)),
		)
	}
}

func (a *syncLocalRepoDoctorAccumulator) checkStaleComponentDirs(doc *bom.BOM, repoRoot string) {
	expected := map[string]struct{}{}
	if doc != nil {
		for _, component := range doc.Packages() {
			expected[component.Name] = struct{}{}
		}
	}
	for _, rootName := range []string{localrepo.InputsDirName, localrepo.PatchesDirName} {
		root := filepath.Join(repoRoot, rootName)
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			a.error(
				"localRepoInvalid",
				"",
				"",
				root,
				fmt.Sprintf("read local repo %s directory: %v", rootName, err),
				"Fix the local repo directory permissions or layout.",
			)
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if _, ok := expected[entry.Name()]; ok {
				continue
			}
			path := filepath.Join(root, entry.Name())
			a.warning(
				"staleComponentDirectory",
				entry.Name(),
				"",
				path,
				fmt.Sprintf("local repo %s directory belongs to component %q, which is not present in the current BOM", rootName, entry.Name()),
				"Remove the stale component directory or update the BOM if it is still intended.",
			)
		}
	}
}

func syncLocalRepoDoctorPatchCount(repo *localrepo.Repo, _ *bom.BOM) int {
	if repo == nil {
		return 0
	}
	patchesRoot := filepath.Join(repo.Root, localrepo.PatchesDirName)
	total := 0
	_ = filepath.WalkDir(patchesRoot, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		total++
		return nil
	})
	return total
}

func syncLocalRepoDoctorSuggested(out syncLocalRepoDoctorOutput) []string {
	seen := map[string]struct{}{}
	suggested := make([]string, 0, len(out.Issues)+1)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		suggested = append(suggested, value)
	}
	for _, issue := range out.Issues {
		add(issue.SuggestedFix)
	}
	if out.Summary.Errors > 0 {
		add("Fix blocking issues, then rerun sealos sync local-repo doctor.")
	} else {
		add("Run sealos sync validate with the same BOM, local repo, and package-source flags before render/apply.")
	}
	return suggested
}

func syncLocalRepoDoctorIsGeneratedPlaceholder(input syncLocalRepoInitInput, data []byte) bool {
	actual := syncLocalRepoDoctorNormalizeTemplate(string(data))
	expected := syncLocalRepoDoctorNormalizeTemplate(syncLocalRepoInputTemplate(input))
	return actual != "" && actual == expected
}

func syncLocalRepoDoctorNormalizeTemplate(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.TrimSpace(value)
}

func syncLocalRepoDoctorInputLooksSecret(input syncLocalRepoInitInput, binding string) bool {
	if _, ok := syncLocalRepoSecretHintForInput(input); ok {
		return true
	}
	return syncLocalRepoDoctorPathLooksSecret(input.Path) || syncLocalRepoDoctorPathLooksSecret(filepath.Base(binding))
}

func syncLocalRepoDoctorPathLooksSecret(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	switch {
	case strings.Contains(lower, "secret"):
		return true
	case strings.Contains(lower, "credential"):
		return true
	case strings.Contains(lower, "password"):
		return true
	case strings.Contains(lower, "token"):
		return true
	case strings.Contains(lower, "cert"):
		return true
	case strings.Contains(lower, "key"):
		return true
	default:
		return false
	}
}

func syncLocalRepoDoctorIsManifestPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}

func syncLocalRepoDoctorAllowedSecretResourceKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "Secret", "ExternalSecret", "ClusterExternalSecret", "SealedSecret":
		return true
	default:
		return false
	}
}

type syncLocalRepoDoctorSeverity string

const (
	syncLocalRepoDoctorSeverityError   syncLocalRepoDoctorSeverity = "error"
	syncLocalRepoDoctorSeverityWarning syncLocalRepoDoctorSeverity = "warning"
)

type syncLocalRepoDoctorIssue struct {
	Severity     syncLocalRepoDoctorSeverity `json:"severity" yaml:"severity"`
	Code         string                      `json:"code" yaml:"code"`
	Component    string                      `json:"component,omitempty" yaml:"component,omitempty"`
	Input        string                      `json:"input,omitempty" yaml:"input,omitempty"`
	Path         string                      `json:"path,omitempty" yaml:"path,omitempty"`
	Message      string                      `json:"message" yaml:"message"`
	SuggestedFix string                      `json:"suggestedFix,omitempty" yaml:"suggestedFix,omitempty"`
}

type syncLocalRepoDoctorSummary struct {
	Components      int `json:"components" yaml:"components"`
	Inputs          int `json:"inputs" yaml:"inputs"`
	RequiredInputs  int `json:"requiredInputs" yaml:"requiredInputs"`
	Resources       int `json:"resources" yaml:"resources"`
	Patches         int `json:"patches" yaml:"patches"`
	Errors          int `json:"errors" yaml:"errors"`
	Warnings        int `json:"warnings" yaml:"warnings"`
	PlaceholderHits int `json:"placeholderHits" yaml:"placeholderHits"`
}

type syncLocalRepoDoctorOutput struct {
	Passed                bool                       `json:"passed" yaml:"passed"`
	LocalRepo             string                     `json:"localRepo" yaml:"localRepo"`
	LocalRepoMetadata     string                     `json:"localRepoMetadataPath,omitempty" yaml:"localRepoMetadataPath,omitempty"`
	LocalRepoRevision     string                     `json:"localRepoRevision,omitempty" yaml:"localRepoRevision,omitempty"`
	LocalRepoRevisionPath string                     `json:"localRepoRevisionPath,omitempty" yaml:"localRepoRevisionPath,omitempty"`
	LocalInputRevision    string                     `json:"localInputRevision,omitempty" yaml:"localInputRevision,omitempty"`
	BOMPath               string                     `json:"bomPath" yaml:"bomPath"`
	BOMDigest             string                     `json:"bomDigest,omitempty" yaml:"bomDigest,omitempty"`
	ReleaseChannelPath    string                     `json:"releaseChannelPath,omitempty" yaml:"releaseChannelPath,omitempty"`
	ReleaseSource         string                     `json:"releaseSource,omitempty" yaml:"releaseSource,omitempty"`
	ReleaseLine           string                     `json:"releaseLine,omitempty" yaml:"releaseLine,omitempty"`
	Channel               string                     `json:"channel,omitempty" yaml:"channel,omitempty"`
	BOMName               string                     `json:"bomName" yaml:"bomName"`
	Revision              string                     `json:"revision" yaml:"revision"`
	Summary               syncLocalRepoDoctorSummary `json:"summary" yaml:"summary"`
	Issues                []syncLocalRepoDoctorIssue `json:"issues,omitempty" yaml:"issues,omitempty"`
	Suggested             []string                   `json:"suggested,omitempty" yaml:"suggested,omitempty"`
}

type syncLocalRepoDoctorAccumulator struct {
	out syncLocalRepoDoctorOutput
}

func newSyncLocalRepoDoctorCmd() *cobra.Command {
	var flags struct {
		clusterName        string
		bomFile            string
		releaseChannelFile string
		releaseSource      string
		releaseLine        string
		channel            string
		localRepo          string
		packageSources     []string
		output             string
	}

	cmd := &cobra.Command{
		Use:          "doctor",
		Short:        "Inspect a cluster-local repo for unresolved templates, unsafe Secret handling, and stale component content",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := runSyncLocalRepoDoctor(syncLocalRepoDoctorOptions{
				ClusterName:        flags.clusterName,
				BOMPath:            flags.bomFile,
				ReleaseChannelPath: flags.releaseChannelFile,
				ReleaseSource:      flags.releaseSource,
				ReleaseLine:        flags.releaseLine,
				Channel:            flags.channel,
				LocalRepoPath:      flags.localRepo,
				PackageSources:     flags.packageSources,
			})
			if err := writeSyncOutput(cmd, out, flags.output, "local repo doctor result"); err != nil {
				return err
			}
			if !out.Passed {
				return errors.New("local repo doctor found blocking issues")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "name of cluster whose runtime package cache should be used")
	addSyncTargetFlags(cmd, &flags.bomFile, &flags.releaseChannelFile, &flags.releaseSource, &flags.releaseLine, &flags.channel, "path to the BOM file used to discover package input contracts")
	cmd.Flags().StringVar(&flags.localRepo, "local-repo", "", "local repo directory to inspect")
	cmd.Flags().StringSliceVar(&flags.packageSources, "package-source", nil, "override a BOM component package source as component=dir for local repo inspection")
	addSyncOutputFlag(cmd, &flags.output)
	if err := cmd.MarkFlagRequired("local-repo"); err != nil {
		panic(err)
	}
	return cmd
}

func newSyncLocalRepoInitCmd() *cobra.Command {
	var flags struct {
		clusterName        string
		bomFile            string
		releaseChannelFile string
		releaseSource      string
		releaseLine        string
		channel            string
		outputDir          string
		packageSources     []string
		overwrite          bool
		output             string
	}

	cmd := &cobra.Command{
		Use:          "init",
		Short:        "Initialize a cluster-local repo skeleton from a BOM and component package inputs",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := runSyncLocalRepoInit(syncLocalRepoInitOptions{
				ClusterName:        flags.clusterName,
				BOMPath:            flags.bomFile,
				ReleaseChannelPath: flags.releaseChannelFile,
				ReleaseSource:      flags.releaseSource,
				ReleaseLine:        flags.releaseLine,
				Channel:            flags.channel,
				OutputDir:          flags.outputDir,
				PackageSources:     flags.packageSources,
				Overwrite:          flags.overwrite,
				CreatedAt:          time.Now().UTC(),
			})
			if err != nil {
				return err
			}
			return writeSyncOutput(cmd, out, flags.output, "local repo init result")
		},
	}
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "name of cluster whose runtime package cache should be used")
	addSyncTargetFlags(cmd, &flags.bomFile, &flags.releaseChannelFile, &flags.releaseSource, &flags.releaseLine, &flags.channel, "path to the BOM file used to discover package input contracts")
	cmd.Flags().StringVar(&flags.outputDir, "output-dir", "", "local repo directory to create or update")
	cmd.Flags().StringSliceVar(&flags.packageSources, "package-source", nil, "override a BOM component package source as component=dir for local initialization")
	cmd.Flags().BoolVar(&flags.overwrite, "overwrite", false, "overwrite generated skeleton files that already exist")
	addSyncOutputFlag(cmd, &flags.output)
	if err := cmd.MarkFlagRequired("output-dir"); err != nil {
		panic(err)
	}
	return cmd
}

func runSyncLocalRepoInit(opts syncLocalRepoInitOptions) (syncLocalRepoInitOutput, error) {
	target, resolved, err := resolveSyncLocalRepoBOMPackages(opts.ClusterName, syncTargetOptions{
		BOMPath:            opts.BOMPath,
		ReleaseChannelPath: opts.ReleaseChannelPath,
		ReleaseSource:      opts.ReleaseSource,
		ReleaseLine:        opts.ReleaseLine,
		Channel:            opts.Channel,
	}, opts.PackageSources)
	if err != nil {
		return syncLocalRepoInitOutput{}, err
	}
	doc := target.BOM

	root, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return syncLocalRepoInitOutput{}, fmt.Errorf("resolve local repo output dir %q: %w", opts.OutputDir, err)
	}
	writer := &syncLocalRepoInitWriter{
		root:      root,
		overwrite: opts.Overwrite,
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return syncLocalRepoInitOutput{}, fmt.Errorf("create local repo %q: %w", root, err)
	}
	if err := writer.ensureDirs(
		localrepo.InputsDirName,
		localrepo.ResourcesDirName,
		filepath.Join(localrepo.ResourcesDirName, "secrets"),
		localrepo.PatchesDirName,
		localrepo.PolicyDirName,
		localrepo.RevisionsDirName,
	); err != nil {
		return syncLocalRepoInitOutput{}, err
	}

	out := syncLocalRepoInitOutput{
		LocalRepo:             root,
		LocalRepoMetadata:     filepath.Join(root, localrepo.RepoFileName),
		LocalRepoRevisionPath: filepath.Join(root, localrepo.RevisionsDirName, localrepo.CurrentRevisionFileName),
		BOMPath:               target.BOMPath,
		BOMDigest:             syncLocalRepoTargetBOMDigest(target),
		ReleaseChannelPath:    strings.TrimSpace(target.ReleaseChannelPath),
		ReleaseSource:         strings.TrimSpace(target.ReleaseSource),
		ReleaseLine:           strings.TrimSpace(target.ReleaseLine),
		Channel:               string(target.Channel),
		BOMName:               doc.Metadata.Name,
		Revision:              doc.Spec.Revision,
		Components:            doc.PackageCount(),
	}
	targetMetadata := syncLocalRepoTargetMetadataForTarget(opts.ClusterName, target)
	if err := writer.writeYAML(localrepo.RepoFileName, syncLocalRepoMetadata(targetMetadata)); err != nil {
		return syncLocalRepoInitOutput{}, err
	}
	if err := writer.writeYAML(filepath.Join(localrepo.PolicyDirName, ownership.LocalPatchPolicyFileName), ownership.DefaultLocalPatchPolicyDocument()); err != nil {
		return syncLocalRepoInitOutput{}, err
	}
	if err := writer.writeFile("README.md", syncLocalRepoReadme(), 0o644); err != nil {
		return syncLocalRepoInitOutput{}, err
	}

	inputs := collectSyncLocalRepoInitInputs(doc, resolved)
	for _, input := range inputs {
		if input.Required {
			out.Summary.RequiredInputs++
		} else {
			out.Summary.OptionalInputs++
		}
		out.Inputs = append(out.Inputs, input)
		if hint, ok := syncLocalRepoSecretHintForInput(input); ok {
			out.SecretHints = append(out.SecretHints, hint)
		}
		if err := writer.writeFile(input.Path, syncLocalRepoInputTemplate(input), syncLocalRepoInputFileMode(input)); err != nil {
			return syncLocalRepoInitOutput{}, err
		}
	}
	initializedRepo, err := localrepo.Load(root)
	if err != nil {
		return syncLocalRepoInitOutput{}, err
	}
	out.LocalRepoRevision = initializedRepo.Revision
	out.LocalInputRevision = initializedRepo.InputRevision
	if err := writer.writeYAML(filepath.Join(localrepo.RevisionsDirName, localrepo.CurrentRevisionFileName), syncLocalRepoCurrentRevision(targetMetadata, initializedRepo, opts.CreatedAt)); err != nil {
		return syncLocalRepoInitOutput{}, err
	}
	finalRepo, err := localrepo.Load(root)
	if err != nil {
		return syncLocalRepoInitOutput{}, err
	}
	out.LocalRepoRevision = finalRepo.Revision
	out.LocalInputRevision = finalRepo.InputRevision
	sort.Slice(out.SecretHints, func(i, j int) bool {
		if out.SecretHints[i].Component != out.SecretHints[j].Component {
			return out.SecretHints[i].Component < out.SecretHints[j].Component
		}
		if out.SecretHints[i].Input != out.SecretHints[j].Input {
			return out.SecretHints[i].Input < out.SecretHints[j].Input
		}
		return out.SecretHints[i].Path < out.SecretHints[j].Path
	})
	out.WrittenFiles = writer.written
	out.SkippedFiles = writer.skipped
	out.Summary.SecretHints = len(out.SecretHints)
	out.Summary.WrittenFiles = len(out.WrittenFiles)
	out.Summary.SkippedFiles = len(out.SkippedFiles)
	out.NextSteps = []string{
		"Fill required input templates under inputs/.",
		"Create real Secret manifests or external-secret references under resources/; do not put Secret bytes in package or BOM files.",
		"Run sealos sync local-repo doctor --file <bom.yaml> --local-repo <local-repo> with the same package-source flags.",
		"Run sealos sync validate --file <bom.yaml> --local-repo <local-repo> with the same package-source flags.",
		"Run sealos sync render, then sealos sync plan before sync apply.",
	}
	return out, nil
}

func collectSyncLocalRepoInitInputs(doc *bom.BOM, resolved map[string]*packageformat.ComponentPackage) []syncLocalRepoInitInput {
	var inputs []syncLocalRepoInitInput
	for _, component := range doc.Packages() {
		pkg, ok := resolved[component.Name]
		if !ok || pkg == nil {
			continue
		}
		for _, input := range pkg.Spec.Inputs {
			filename := input.Name + syncLocalRepoInputExtension(input)
			inputs = append(inputs, syncLocalRepoInitInput{
				Component: component.Name,
				Name:      input.Name,
				Type:      string(input.Type),
				Format:    input.Format,
				Required:  input.Required,
				Path:      filepath.ToSlash(filepath.Join(localrepo.InputsDirName, component.Name, filename)),
			})
		}
	}
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].Component != inputs[j].Component {
			return inputs[i].Component < inputs[j].Component
		}
		if inputs[i].Required != inputs[j].Required {
			return inputs[i].Required
		}
		return inputs[i].Name < inputs[j].Name
	})
	return inputs
}

func syncLocalRepoInputTemplate(input syncLocalRepoInitInput) string {
	header := strings.Join([]string{
		"# Generated by sealos sync local-repo init.",
		"# Fill this cluster-local value before running sync render/apply.",
		fmt.Sprintf("# component: %s", input.Component),
		fmt.Sprintf("# input: %s", input.Name),
		fmt.Sprintf("# type: %s", input.Type),
		fmt.Sprintf("# required: %t", input.Required),
	}, "\n")
	if input.Format != "" {
		header += "\n# format: " + input.Format
	}
	switch strings.ToLower(strings.TrimSpace(input.Format)) {
	case "yaml", "yml":
		return header + "\nvalue: \"\"\n"
	case "json":
		return header + "\n{}\n"
	default:
		switch packageformat.InputType(input.Type) {
		case packageformat.InputEnv:
			return header + "\nKEY=value\n"
		default:
			return header + "\n"
		}
	}
}

func syncLocalRepoInputExtension(input packageformat.Input) string {
	switch strings.ToLower(strings.TrimSpace(input.Format)) {
	case "yaml", "yml":
		return ".yaml"
	case "json":
		return ".json"
	case "env":
		return ".env"
	default:
		if ext := filepath.Ext(input.Path); ext != "" {
			return ext
		}
		return ".txt"
	}
}

func syncLocalRepoInputFileMode(input syncLocalRepoInitInput) os.FileMode {
	if _, ok := syncLocalRepoSecretHintForInput(input); ok {
		return 0o600
	}
	return 0o644
}

func syncLocalRepoSecretHintForInput(input syncLocalRepoInitInput) (syncLocalRepoInitSecretHint, bool) {
	needle := strings.ToLower(strings.Join([]string{input.Name, input.Path, input.Type, input.Format}, " "))
	if !strings.Contains(needle, "secret") &&
		!strings.Contains(needle, "password") &&
		!strings.Contains(needle, "credential") &&
		!strings.Contains(needle, "token") &&
		!strings.Contains(needle, "cert") &&
		!strings.Contains(needle, "key") {
		return syncLocalRepoInitSecretHint{}, false
	}
	return syncLocalRepoInitSecretHint{
		Component: input.Component,
		Input:     input.Name,
		Path:      filepath.ToSlash(filepath.Join(localrepo.ResourcesDirName, "secrets", input.Component+"-"+input.Name+".yaml")),
		Reason:    "input name or path looks secret-bearing; initialize the real Secret manifest or external reference in resources/, not in the package or BOM",
	}, true
}

func syncLocalRepoReadme() string {
	return strings.Join([]string{
		"# Local Repo Skeleton",
		"",
		"This directory was initialized by sealos sync local-repo init.",
		"",
		"Fill required package input templates under inputs/.",
		"",
		"Place cluster-local Secret manifests or external-secret references under",
		"resources/ when needed. Do not put Secret bytes in package artifacts or BOM",
		"files. Prefer private file permissions such as 0600 for Secret manifests.",
		"",
		"Run sealos sync local-repo doctor, then sealos sync validate before render/apply.",
		"",
	}, "\n")
}

func syncLocalRepoMetadata(target syncLocalRepoTargetMetadata) *localrepo.LocalRepoDocument {
	return localrepo.NewDocument(
		syncLocalRepoName(target),
		target.Cluster,
		target.DistributionLine,
		target.Channel,
		target.BOMName,
		target.BOMRevision,
	)
}

func syncLocalRepoCurrentRevision(target syncLocalRepoTargetMetadata, repo *localrepo.Repo, createdAt time.Time) *localrepo.LocalRepoRevisionDocument {
	localInputRevision := ""
	localRepoRevision := ""
	if repo != nil {
		localInputRevision = repo.InputRevision
		localRepoRevision = repo.Revision
	}
	return localrepo.NewRevisionDocument("current", localrepo.LocalRepoRevisionSpec{
		Cluster:          strings.TrimSpace(target.Cluster),
		DistributionLine: strings.TrimSpace(target.DistributionLine),
		Channel:          strings.TrimSpace(target.Channel),
		BOM: localrepo.BOMReference{
			Name:     strings.TrimSpace(target.BOMName),
			Revision: strings.TrimSpace(target.BOMRevision),
			Digest:   strings.TrimSpace(target.BOMDigest),
		},
		LocalInputRevision: localInputRevision,
		Digest:             localRepoRevision,
		Audit: localrepo.AuditFields{
			CreatedAt: createdAt.UTC().Format(time.RFC3339),
			Command:   "sealos sync local-repo init",
		},
	})
}

func syncLocalRepoTargetMetadataForTarget(clusterName string, target *syncResolvedTarget) syncLocalRepoTargetMetadata {
	metadata := syncLocalRepoTargetMetadata{
		Cluster: strings.TrimSpace(clusterName),
	}
	if metadata.Cluster == "" {
		metadata.Cluster = "default"
	}
	if target == nil || target.BOM == nil {
		return metadata
	}
	metadata.DistributionLine = strings.TrimSpace(target.BOM.Metadata.Name)
	if target.ReleaseChannelDocument != nil {
		metadata.DistributionLine = target.ReleaseChannelDocument.Distribution()
		metadata.Channel = string(target.ReleaseChannelDocument.Spec.Channel)
	} else if target.Channel != "" {
		metadata.Channel = string(target.Channel)
	}
	metadata.BOMName = strings.TrimSpace(target.BOM.Metadata.Name)
	metadata.BOMRevision = strings.TrimSpace(target.BOM.Spec.Revision)
	metadata.BOMDigest = syncLocalRepoTargetBOMDigest(target)
	return metadata
}

func syncLocalRepoTargetBOMDigest(target *syncResolvedTarget) string {
	if target == nil {
		return ""
	}
	if strings.TrimSpace(target.BOMDigest) != "" {
		return strings.TrimSpace(target.BOMDigest)
	}
	if strings.TrimSpace(target.BOMPath) == "" || isSyncRemoteLocation(target.BOMPath) {
		return ""
	}
	data, err := os.ReadFile(target.BOMPath)
	if err != nil {
		return ""
	}
	return digest.Canonical.FromBytes(data).String()
}

func syncLocalRepoName(target syncLocalRepoTargetMetadata) string {
	cluster := strings.TrimSpace(target.Cluster)
	line := strings.TrimSpace(target.DistributionLine)
	switch {
	case cluster != "" && line != "":
		return cluster + "-" + line
	case cluster != "":
		return cluster
	case line != "":
		return line
	default:
		return "local-repo"
	}
}

func (w *syncLocalRepoInitWriter) ensureDirs(dirs ...string) error {
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(w.root, filepath.FromSlash(dir)), 0o755); err != nil {
			return fmt.Errorf("create local repo directory %q: %w", dir, err)
		}
	}
	return nil
}

func (w *syncLocalRepoInitWriter) writeYAML(rel string, value interface{}) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", rel, err)
	}
	return w.writeFile(rel, string(data), 0o644)
}

func (w *syncLocalRepoInitWriter) writeFile(rel, content string, mode os.FileMode) error {
	rel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if rel == "." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return fmt.Errorf("invalid local repo relative path %q", rel)
	}
	path := filepath.Join(w.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %q: %w", rel, err)
	}
	if _, err := os.Stat(path); err == nil && !w.overwrite {
		w.skipped = append(w.skipped, rel)
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat %q: %w", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return fmt.Errorf("write %q: %w", rel, err)
	}
	w.written = append(w.written, rel)
	return nil
}
