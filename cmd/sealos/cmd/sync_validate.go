package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/ownership"
	"github.com/labring/sealos/pkg/distribution/packageformat"
)

type syncValidateSeverity string

const (
	syncValidateSeverityError   syncValidateSeverity = "error"
	syncValidateSeverityWarning syncValidateSeverity = "warning"
)

type syncValidateIssue struct {
	Severity  syncValidateSeverity `json:"severity" yaml:"severity"`
	Code      string               `json:"code" yaml:"code"`
	Component string               `json:"component,omitempty" yaml:"component,omitempty"`
	Path      string               `json:"path,omitempty" yaml:"path,omitempty"`
	Message   string               `json:"message" yaml:"message"`
}

type syncValidateSummary struct {
	Components       int `json:"components" yaml:"components"`
	PackagesResolved int `json:"packagesResolved" yaml:"packagesResolved"`
	RequiredInputs   int `json:"requiredInputs" yaml:"requiredInputs"`
	BoundInputs      int `json:"boundInputs" yaml:"boundInputs"`
	HostInputs       int `json:"hostInputs" yaml:"hostInputs"`
	Hosts            int `json:"hosts" yaml:"hosts"`
	LocalResources   int `json:"localResources" yaml:"localResources"`
	LocalPatches     int `json:"localPatches" yaml:"localPatches"`
	Errors           int `json:"errors" yaml:"errors"`
	Warnings         int `json:"warnings" yaml:"warnings"`
}

type syncValidatePackageOutput struct {
	Component   string `json:"component" yaml:"component"`
	PackageName string `json:"packageName" yaml:"packageName"`
	Version     string `json:"version" yaml:"version"`
	Class       string `json:"class" yaml:"class"`
	Source      string `json:"source,omitempty" yaml:"source,omitempty"`
}

type syncValidateOutput struct {
	Passed            bool                        `json:"passed" yaml:"passed"`
	ClusterName       string                      `json:"clusterName" yaml:"clusterName"`
	BOMPath           string                      `json:"bomPath" yaml:"bomPath"`
	LocalRepo         string                      `json:"localRepo,omitempty" yaml:"localRepo,omitempty"`
	ExecutionTopology hydrate.ExecutionTopology   `json:"executionTopology,omitempty" yaml:"executionTopology,omitempty"`
	Summary           syncValidateSummary         `json:"summary" yaml:"summary"`
	Packages          []syncValidatePackageOutput `json:"packages,omitempty" yaml:"packages,omitempty"`
	Issues            []syncValidateIssue         `json:"issues,omitempty" yaml:"issues,omitempty"`
	LocalPolicy       string                      `json:"localPolicy,omitempty" yaml:"localPolicy,omitempty"`
	LocalRepoRev      string                      `json:"localRepoRevision,omitempty" yaml:"localRepoRevision,omitempty"`
}

type syncValidateAccumulator struct {
	out syncValidateOutput
}

func (a *syncValidateAccumulator) issue(severity syncValidateSeverity, code, component, path, message string) {
	a.out.Issues = append(a.out.Issues, syncValidateIssue{
		Severity:  severity,
		Code:      code,
		Component: strings.TrimSpace(component),
		Path:      strings.TrimSpace(path),
		Message:   strings.TrimSpace(message),
	})
	switch severity {
	case syncValidateSeverityError:
		a.out.Summary.Errors++
	case syncValidateSeverityWarning:
		a.out.Summary.Warnings++
	}
}

func (a *syncValidateAccumulator) error(code, component, path, message string) {
	a.issue(syncValidateSeverityError, code, component, path, message)
}

func (a *syncValidateAccumulator) warning(code, component, path, message string) {
	a.issue(syncValidateSeverityWarning, code, component, path, message)
}

func (a *syncValidateAccumulator) finalize() syncValidateOutput {
	sort.Slice(a.out.Issues, func(i, j int) bool {
		if a.out.Issues[i].Severity != a.out.Issues[j].Severity {
			return a.out.Issues[i].Severity < a.out.Issues[j].Severity
		}
		if a.out.Issues[i].Component != a.out.Issues[j].Component {
			return a.out.Issues[i].Component < a.out.Issues[j].Component
		}
		if a.out.Issues[i].Code != a.out.Issues[j].Code {
			return a.out.Issues[i].Code < a.out.Issues[j].Code
		}
		return a.out.Issues[i].Path < a.out.Issues[j].Path
	})
	sort.Slice(a.out.Packages, func(i, j int) bool {
		return a.out.Packages[i].Component < a.out.Packages[j].Component
	})
	a.out.Passed = a.out.Summary.Errors == 0
	return a.out
}

func newSyncValidateCmd() *cobra.Command {
	var flags struct {
		clusterName    string
		bomFile        string
		localRepo      string
		packageSources []string
		output         string
	}

	cmd := &cobra.Command{
		Use:          "validate",
		Short:        "Validate a BOM, local package sources, and local repo bindings before render/apply",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := runSyncValidate(syncValidateOptions{
				ClusterName:    flags.clusterName,
				BOMPath:        flags.bomFile,
				LocalRepoPath:  flags.localRepo,
				PackageSources: flags.packageSources,
			})
			if err := writeSyncOutput(cmd, out, flags.output, "validate result"); err != nil {
				return err
			}
			if !out.Passed {
				return errors.New("sync validation failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "name of cluster whose inventory should be used for topology validation")
	cmd.Flags().StringVarP(&flags.bomFile, "file", "f", "", "path to the BOM file to validate")
	cmd.Flags().StringVar(&flags.localRepo, "local-repo", "", "optional cluster-local repo root to validate against package inputs and local patch policy")
	cmd.Flags().StringSliceVar(&flags.packageSources, "package-source", nil, "override a BOM component package source as component=dir for local validation")
	addSyncOutputFlag(cmd, &flags.output)
	if err := cmd.MarkFlagRequired("file"); err != nil {
		panic(err)
	}
	return cmd
}

type syncValidateOptions struct {
	ClusterName    string
	BOMPath        string
	LocalRepoPath  string
	PackageSources []string
}

func runSyncValidate(opts syncValidateOptions) syncValidateOutput {
	acc := syncValidateAccumulator{}
	acc.out.ClusterName = strings.TrimSpace(opts.ClusterName)
	if acc.out.ClusterName == "" {
		acc.out.ClusterName = "default"
	}
	acc.out.BOMPath = strings.TrimSpace(opts.BOMPath)
	acc.out.LocalRepo = strings.TrimSpace(opts.LocalRepoPath)

	topology, err := loadSyncExecutionTopology(acc.out.ClusterName)
	if err != nil {
		acc.error("topologyInvalid", "", "", err.Error())
	} else if topology != nil {
		acc.out.ExecutionTopology = topology.snapshot()
		acc.out.Summary.Hosts = len(acc.out.ExecutionTopology.AllNodes)
	}

	doc, err := bom.LoadFile(opts.BOMPath)
	if err != nil {
		acc.error("bomInvalid", "", opts.BOMPath, err.Error())
		return acc.finalize()
	}
	acc.out.Summary.Components = len(doc.Spec.Components)

	localRoots, localPackages, err := resolveSyncPackageSources(doc, opts.PackageSources)
	if err != nil {
		acc.error("packageSourceInvalid", "", "", err.Error())
		return acc.finalize()
	}

	var fallbackLoader packageformat.Loader
	if len(localRoots) < len(doc.Spec.Components) {
		fallbackLoader, _, err = newSyncCachedArtifactResolver(acc.out.ClusterName)
		if err != nil {
			acc.error("packageResolutionFailed", "", "", err.Error())
			return acc.finalize()
		}
	}
	resolved, err := doc.ResolveComponentPackages(syncPackageLoader{
		local:    localPackages,
		fallback: fallbackLoader,
	})
	if err != nil {
		acc.error("packageResolutionFailed", "", "", err.Error())
	} else {
		acc.out.Summary.PackagesResolved = len(resolved)
		acc.addPackageOutputs(doc, resolved, localRoots)
	}

	var repo *localrepo.Repo
	if strings.TrimSpace(opts.LocalRepoPath) != "" {
		repo, err = localrepo.Load(opts.LocalRepoPath)
		if err != nil {
			acc.error("localRepoInvalid", "", opts.LocalRepoPath, err.Error())
		} else {
			acc.out.LocalRepo = repo.Root
			acc.out.LocalRepoRev = repo.Revision
			acc.out.Summary.LocalResources = len(repo.Resources())
			acc.out.Summary.LocalPatches = syncValidatePatchCount(repo, doc)
			acc.validateLocalRepoResources(repo)
		}
	}

	if len(resolved) > 0 {
		acc.validatePackages(doc, resolved, localRoots, repo, topology)
	}
	if repo != nil {
		acc.validateLocalPatchPolicy(repo)
	}

	return acc.finalize()
}

func (a *syncValidateAccumulator) addPackageOutputs(doc *bom.BOM, resolved map[string]*packageformat.ComponentPackage, localRoots map[string]string) {
	for _, component := range doc.Spec.Components {
		pkg, ok := resolved[component.Name]
		if !ok {
			continue
		}
		a.out.Packages = append(a.out.Packages, syncValidatePackageOutput{
			Component:   component.Name,
			PackageName: pkg.Metadata.Name,
			Version:     pkg.Spec.Version,
			Class:       string(pkg.Spec.Class),
			Source:      localRoots[component.Name],
		})
	}
}

func (a *syncValidateAccumulator) validatePackages(doc *bom.BOM, resolved map[string]*packageformat.ComponentPackage, localRoots map[string]string, repo *localrepo.Repo, topology *syncExecutionTopology) {
	for _, component := range doc.Spec.Components {
		pkg, ok := resolved[component.Name]
		if !ok {
			continue
		}
		root := localRoots[component.Name]
		a.validatePackageHooks(component.Name, root, pkg)
		a.validatePackageInputs(component.Name, pkg, repo, topology)
		a.validatePackageTargets(component.Name, pkg, topology)
	}
}

func (a *syncValidateAccumulator) validatePackageHooks(componentName, root string, pkg *packageformat.ComponentPackage) {
	if pkg == nil {
		return
	}
	if strings.TrimSpace(root) == "" {
		return
	}
	for _, hook := range pkg.Spec.Hooks {
		hookPath := filepath.Join(root, filepath.FromSlash(hook.Path))
		info, err := os.Stat(hookPath)
		if err != nil {
			a.error("hookInvalid", componentName, hook.Path, fmt.Sprintf("stat hook %q: %v", hook.Path, err))
			continue
		}
		if info.IsDir() {
			a.error("hookInvalid", componentName, hook.Path, fmt.Sprintf("hook %q must reference a file, got directory", hook.Path))
			continue
		}
		if info.Mode()&0o111 == 0 {
			a.error("hookNotExecutable", componentName, hook.Path, fmt.Sprintf("hook %q should be executable before packaging", hook.Path))
		}
	}
}

func (a *syncValidateAccumulator) validatePackageInputs(componentName string, pkg *packageformat.ComponentPackage, repo *localrepo.Repo, topology *syncExecutionTopology) {
	if pkg == nil {
		return
	}
	for _, input := range pkg.Spec.Inputs {
		if input.Required {
			a.out.Summary.RequiredInputs++
		}
		if repo == nil {
			if input.Required {
				a.error("requiredInputMissing", componentName, input.Name, fmt.Sprintf("required input %q is not bound in local repo", input.Name))
			}
			continue
		}
		binding, bound := repo.BindingFor(componentName, input)
		if bound {
			a.out.Summary.BoundInputs++
			a.validateLocalInputFile(componentName, input.Name, binding)
		} else if input.Required {
			a.error("requiredInputMissing", componentName, input.Name, fmt.Sprintf("required input %q is not bound in local repo", input.Name))
		}

		hostBindings := repo.HostBindingsFor(componentName, input)
		if len(hostBindings) == 0 {
			continue
		}
		hosts := make([]string, 0, len(hostBindings))
		for host := range hostBindings {
			hosts = append(hosts, host)
		}
		sort.Strings(hosts)
		for _, host := range hosts {
			hostBinding := hostBindings[host]
			a.out.Summary.HostInputs++
			if strings.TrimSpace(host) == "" {
				a.error("hostInputInvalid", componentName, input.Name, "host-scoped input uses an empty host directory")
			}
			if !bound && input.Required {
				a.warning("hostInputWithoutDefault", componentName, input.Name, fmt.Sprintf("host-scoped input for host %q exists, but required default input %q is missing", host, input.Name))
			}
			if topology != nil && !syncValidateTopologyHasHost(topology, host) {
				a.error("hostInputUnknownHost", componentName, input.Name, fmt.Sprintf("host-scoped input for host %q is not present in cluster inventory", host))
			}
			a.validateLocalInputFile(componentName, input.Name, hostBinding)
		}
	}
}

func (a *syncValidateAccumulator) validateLocalInputFile(componentName, inputName, path string) {
	info, err := os.Stat(path)
	if err != nil {
		a.error("inputInvalid", componentName, inputName, fmt.Sprintf("stat input binding %q: %v", path, err))
		return
	}
	if info.IsDir() {
		a.warning("inputIsDirectory", componentName, inputName, fmt.Sprintf("input binding %q is a directory; verify the package expects a directory payload", path))
	}
	if syncValidatePathLooksSecret(inputName) || syncValidatePathLooksSecret(filepath.Base(path)) {
		a.validateSecretBearingFile(componentName, path)
	}
}

func (a *syncValidateAccumulator) validatePackageTargets(componentName string, pkg *packageformat.ComponentPackage, topology *syncExecutionTopology) {
	if pkg == nil {
		return
	}
	for _, hook := range pkg.Spec.Hooks {
		switch hook.Target {
		case packageformat.TargetAllNodes, packageformat.TargetFirstMaster, packageformat.TargetCluster:
		default:
			a.error("targetInvalid", componentName, hook.Path, fmt.Sprintf("hook %q uses unsupported target %q", hook.Name, hook.Target))
			continue
		}
		if topology == nil {
			continue
		}
		if err := syncValidateTargetResolves(topology, hook.Target); err != nil {
			a.error("targetUnresolved", componentName, hook.Path, fmt.Sprintf("hook %q target %q cannot be resolved: %v", hook.Name, hook.Target, err))
		}
	}
}

func (a *syncValidateAccumulator) validateLocalRepoResources(repo *localrepo.Repo) {
	if repo == nil {
		return
	}
	for _, resource := range repo.Resources() {
		if syncValidatePathLooksSecret(resource.RelativePath) {
			a.validateSecretBearingFile("", resource.Path)
		}
	}
}

func (a *syncValidateAccumulator) validateSecretBearingFile(componentName, path string) {
	info, err := os.Stat(path)
	if err != nil {
		a.error("secretFileInvalid", componentName, path, fmt.Sprintf("stat secret-bearing file %q: %v", path, err))
		return
	}
	if info.IsDir() {
		return
	}
	if syncValidateManifestKind(path) == "Secret" && info.Mode().Perm()&0o077 != 0 {
		a.error("secretManifestModeTooOpen", componentName, path, fmt.Sprintf("Secret manifest %q should use a private file mode such as 0600", path))
		return
	}
	if info.Mode().Perm()&0o077 != 0 {
		a.warning("secretFileModeTooOpen", componentName, path, fmt.Sprintf("secret-bearing file %q should not be readable by group or other users; current mode is %s", path, info.Mode().Perm()))
	}
}

func (a *syncValidateAccumulator) validateLocalPatchPolicy(repo *localrepo.Repo) {
	if repo == nil {
		return
	}
	policyDoc := repo.LocalPatchPolicy()
	policy := ownership.DefaultLocalPatchPolicy()
	if policyDoc != nil {
		policy = policyDoc.Spec
		a.out.LocalPolicy = repo.LocalPatchPolicyRelativePath()
	} else {
		a.out.LocalPolicy = string(ownership.LocalPatchPolicySourceBuiltInDefault)
	}
	compatibility, err := localrepo.EvaluatePatchCompatibility(repo, policy)
	if err != nil {
		a.error("localPatchPolicyInvalid", "", repo.LocalPatchPolicyRelativePath(), err.Error())
		return
	}
	for _, issue := range compatibility.InvalidPatches {
		a.error("localPatchPolicyViolation", issue.Component, issue.RelativePath, issue.Reason)
	}
}

func syncValidatePatchCount(repo *localrepo.Repo, doc *bom.BOM) int {
	if repo == nil || doc == nil {
		return 0
	}
	total := 0
	for _, component := range doc.Spec.Components {
		total += len(repo.PatchesFor(component.Name))
	}
	return total
}

func syncValidateTopologyHasHost(topology *syncExecutionTopology, host string) bool {
	if topology == nil {
		return true
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	for _, candidate := range topology.nodeExecutionHosts() {
		if syncValidateHostsMatch(candidate, host) {
			return true
		}
	}
	return false
}

func syncValidateTargetResolves(topology *syncExecutionTopology, target packageformat.ExecutionTarget) error {
	switch target {
	case packageformat.TargetCluster:
		return nil
	case packageformat.TargetAllNodes:
		if len(topology.nodeExecutionHosts()) == 0 {
			return fmt.Errorf("allNodes is empty")
		}
		return nil
	case packageformat.TargetFirstMaster:
		if strings.TrimSpace(topology.firstMaster) == "" {
			return fmt.Errorf("firstMaster is empty")
		}
		if !syncValidateTopologyHasHost(topology, topology.firstMaster) {
			return fmt.Errorf("firstMaster %q is not present in allNodes", topology.firstMaster)
		}
		return nil
	default:
		return fmt.Errorf("unsupported target %q", target)
	}
}

func syncValidateHostsMatch(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	leftHost := syncValidateHostWithoutPort(left)
	rightHost := syncValidateHostWithoutPort(right)
	return leftHost != "" && rightHost != "" && leftHost == rightHost
}

func syncValidateHostWithoutPort(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if before, _, ok := strings.Cut(host, ":"); ok {
		if strings.TrimSpace(before) != "" {
			return strings.TrimSpace(before)
		}
	}
	return host
}

func syncValidatePathLooksSecret(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	switch {
	case strings.Contains(lower, "secret"):
		return true
	case strings.Contains(lower, "credential"):
		return true
	case strings.Contains(lower, "password"):
		return true
	default:
		return false
	}
}

func syncValidateManifestKind(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var header struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return ""
	}
	return strings.TrimSpace(header.Kind)
}
