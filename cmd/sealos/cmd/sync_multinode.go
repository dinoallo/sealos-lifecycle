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
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/labring/sealos/pkg/clusterfile"
	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/compare"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	sealosexec "github.com/labring/sealos/pkg/exec"
	"github.com/labring/sealos/pkg/ssh"
	v1beta1 "github.com/labring/sealos/pkg/types/v1beta1"
	"github.com/labring/sealos/pkg/utils/iputils"
	"github.com/opencontainers/go-digest"
)

type syncRemoteExecutor interface {
	Copy(host, src, dst string) error
	Fetch(host, src, dst string) error
	CmdAsyncWithContext(ctx context.Context, host string, cmds ...string) error
	CmdToString(host, cmd, sep string) (string, error)
}

type syncExecutionTopology struct {
	cluster     *v1beta1.Cluster
	allNodes    []string
	firstMaster string
	hostRoles   map[string][]string
}

var (
	loadSyncExecutionTopology = defaultSyncExecutionTopology
	newSyncRemoteExecutor     = defaultSyncRemoteExecutor
)

type syncTopologyState string

const (
	syncTopologyStateMatched syncTopologyState = "matched"
	syncTopologyStateStale   syncTopologyState = "stale"
	syncTopologyStateMissing syncTopologyState = "missing"
	syncTopologyStateUnknown syncTopologyState = "unknown"
)

type syncTopologyStatus struct {
	State          syncTopologyState         `json:"state"                    yaml:"state"`
	Message        string                    `json:"message,omitempty"        yaml:"message,omitempty"`
	RefreshCommand string                    `json:"refreshCommand,omitempty" yaml:"refreshCommand,omitempty"`
	Bundle         hydrate.ExecutionTopology `json:"bundle,omitempty"         yaml:"bundle,omitempty"`
	Current        hydrate.ExecutionTopology `json:"current,omitempty"        yaml:"current,omitempty"`
	ChangedFields  []string                  `json:"changedFields,omitempty"  yaml:"changedFields,omitempty"`
}

type syncRenderInputState string

const (
	syncRenderInputStateMatched syncRenderInputState = "matched"
	syncRenderInputStateStale   syncRenderInputState = "stale"
	syncRenderInputStateMissing syncRenderInputState = "missing"
	syncRenderInputStateUnknown syncRenderInputState = "unknown"
)

type syncRenderInputStatus struct {
	State         syncRenderInputState     `json:"state"                   yaml:"state"`
	Message       string                   `json:"message,omitempty"       yaml:"message,omitempty"`
	ChangedInputs []syncRenderInputChange  `json:"changedInputs,omitempty" yaml:"changedInputs,omitempty"`
	Provenance    hydrate.RenderProvenance `json:"provenance,omitempty"    yaml:"provenance,omitempty"`
}

type syncRenderInputChange struct {
	Name     string `json:"name"               yaml:"name"`
	Path     string `json:"path,omitempty"     yaml:"path,omitempty"`
	Expected string `json:"expected,omitempty" yaml:"expected,omitempty"`
	Current  string `json:"current,omitempty"  yaml:"current,omitempty"`
	Reason   string `json:"reason"             yaml:"reason"`
}

func syncExecutionTopologyForBundle(
	clusterName string,
	bundle *hydrate.Bundle,
) (*syncExecutionTopology, error) {
	if bundle != nil && !bundle.Spec.ExecutionTopology.Empty() {
		topology, err := syncExecutionTopologyFromSnapshot(bundle.Spec.ExecutionTopology)
		if err != nil {
			return nil, err
		}
		current, err := loadSyncExecutionTopology(clusterName)
		if err != nil {
			return nil, err
		}
		if current != nil && current.cluster != nil {
			topology.cluster = current.cluster
		}
		return topology, nil
	}
	return loadSyncExecutionTopology(clusterName)
}

func syncExecutionTopologyFromSnapshot(
	snapshot hydrate.ExecutionTopology,
) (*syncExecutionTopology, error) {
	normalized := snapshot.Normalize()
	if len(normalized.AllNodes) == 0 {
		return nil, fmt.Errorf("bundle executionTopology.allNodes cannot be empty")
	}
	if strings.TrimSpace(normalized.FirstMaster) == "" {
		return nil, fmt.Errorf("bundle executionTopology.firstMaster cannot be empty")
	}
	rolesByHost := make(map[string][]string, len(normalized.HostRoles))
	for _, item := range normalized.HostRoles {
		if strings.TrimSpace(item.Host) == "" {
			continue
		}
		rolesByHost[item.Host] = slices.Clone(item.Roles)
	}
	return &syncExecutionTopology{
		allNodes:    normalized.AllNodes,
		firstMaster: normalized.FirstMaster,
		hostRoles:   rolesByHost,
	}, nil
}

func syncTopologyStatusForBundle(clusterName string, bundle *hydrate.Bundle) syncTopologyStatus {
	status := syncTopologyStatus{
		RefreshCommand: syncTopologyRefreshCommand(clusterName, nil),
	}
	if bundle != nil {
		status.RefreshCommand = syncTopologyRefreshCommand(
			clusterName,
			&bundle.Spec.RenderProvenance,
		)
	}
	if bundle == nil || bundle.Spec.ExecutionTopology.Empty() {
		status.State = syncTopologyStateMissing
		status.Message = "bundle has no executionTopology snapshot; re-render the bundle to initialize target tracking"
		return status
	}

	bundleSnapshot := bundle.Spec.ExecutionTopology.Normalize()
	status.Bundle = bundleSnapshot

	current, err := defaultSyncExecutionTopology(clusterName)
	if err != nil {
		status.State = syncTopologyStateUnknown
		status.Message = fmt.Sprintf("cannot load current Clusterfile topology: %v", err)
		return status
	}
	if current == nil {
		status.State = syncTopologyStateUnknown
		status.Message = "cannot load current Clusterfile topology"
		return status
	}

	currentSnapshot := current.snapshot()
	status.Current = currentSnapshot
	changedFields := changedSyncTopologyFields(bundleSnapshot, currentSnapshot)
	if len(changedFields) == 0 {
		status.State = syncTopologyStateMatched
		status.Message = "bundle executionTopology matches current Clusterfile topology"
		return status
	}
	status.State = syncTopologyStateStale
	status.ChangedFields = changedFields
	status.Message = "bundle executionTopology differs from current Clusterfile topology; re-run sync render to refresh the target snapshot"
	return status
}

func syncRenderInputStatusForBundle(bundle *hydrate.Bundle) syncRenderInputStatus {
	if bundle == nil || bundle.Spec.RenderProvenance.Empty() {
		return syncRenderInputStatus{
			State:   syncRenderInputStateMissing,
			Message: "bundle has no renderProvenance; re-render the bundle to initialize input freshness tracking",
		}
	}
	provenance := bundle.Spec.RenderProvenance.Normalize()
	status := syncRenderInputStatus{
		State:      syncRenderInputStateMatched,
		Message:    "render inputs match recorded provenance",
		Provenance: provenance,
	}

	var changes []syncRenderInputChange
	if strings.TrimSpace(provenance.ReleaseSource) != "" {
		resolved, err := bom.ResolveReleaseChannelLookup(bom.ReleaseChannelLookupOptions{
			DistributionLine: provenance.DistributionLine,
			Channel:          bom.ReleaseChannel(provenance.ReleaseChannel),
			Source:           provenance.ReleaseSource,
		})
		switch {
		case err != nil:
			changes = append(changes, syncRenderInputChange{
				Name:     "releaseLookup",
				Path:     provenance.ReleaseSource,
				Expected: provenance.BOMDigest,
				Reason:   err.Error(),
			})
		case strings.TrimSpace(resolved.BOMDigest) != strings.TrimSpace(provenance.BOMDigest):
			changes = append(changes, syncRenderInputChange{
				Name:     "releaseLookup",
				Path:     provenance.ReleaseSource,
				Expected: provenance.BOMDigest,
				Current:  resolved.BOMDigest,
				Reason:   "resolved BOM digest mismatch",
			})
		case resolved.BOM.Spec.Revision != bundle.Spec.Revision:
			changes = append(changes, syncRenderInputChange{
				Name:     "releaseLookup",
				Path:     provenance.ReleaseSource,
				Expected: bundle.Spec.Revision,
				Current:  resolved.BOM.Spec.Revision,
				Reason:   "resolved BOM revision mismatch",
			})
		}
	} else if strings.TrimSpace(provenance.ReleaseChannelPath) != "" || strings.TrimSpace(provenance.ReleaseChannelDigest) != "" {
		switch {
		case strings.TrimSpace(provenance.ReleaseChannelPath) == "" || strings.TrimSpace(provenance.ReleaseChannelDigest) == "":
			changes = append(changes, syncRenderInputChange{
				Name:   "releaseChannel",
				Path:   provenance.ReleaseChannelPath,
				Reason: "missing ReleaseChannel provenance",
			})
		default:
			data, err := os.ReadFile(provenance.ReleaseChannelPath)
			if err != nil {
				changes = append(changes, syncRenderInputChange{
					Name:     "releaseChannel",
					Path:     provenance.ReleaseChannelPath,
					Expected: provenance.ReleaseChannelDigest,
					Reason:   err.Error(),
				})
			} else if current := digest.Canonical.FromBytes(data).String(); current != provenance.ReleaseChannelDigest {
				changes = append(changes, syncRenderInputChange{
					Name:     "releaseChannel",
					Path:     provenance.ReleaseChannelPath,
					Expected: provenance.ReleaseChannelDigest,
					Current:  current,
					Reason:   "digest mismatch",
				})
			}
		}
	}

	if strings.TrimSpace(provenance.ReleaseSource) == "" {
		if strings.TrimSpace(provenance.BOMPath) == "" ||
			strings.TrimSpace(provenance.BOMDigest) == "" {
			changes = append(changes, syncRenderInputChange{
				Name:   "bom",
				Path:   provenance.BOMPath,
				Reason: "missing BOM provenance",
			})
		} else if data, err := os.ReadFile(provenance.BOMPath); err != nil {
			changes = append(changes, syncRenderInputChange{
				Name:     "bom",
				Path:     provenance.BOMPath,
				Expected: provenance.BOMDigest,
				Reason:   err.Error(),
			})
		} else if current := digest.Canonical.FromBytes(data).String(); current != provenance.BOMDigest {
			changes = append(changes, syncRenderInputChange{
				Name:     "bom",
				Path:     provenance.BOMPath,
				Expected: provenance.BOMDigest,
				Current:  current,
				Reason:   "digest mismatch",
			})
		}
	}

	if strings.TrimSpace(provenance.LocalRepoPath) != "" {
		repo, err := localrepo.Load(provenance.LocalRepoPath)
		if err != nil {
			changes = append(changes, syncRenderInputChange{
				Name:     "localRepo",
				Path:     provenance.LocalRepoPath,
				Expected: provenance.LocalRepoRevision,
				Reason:   err.Error(),
			})
		} else if repo.Revision != provenance.LocalRepoRevision {
			changes = append(changes, syncRenderInputChange{
				Name:     "localRepo",
				Path:     provenance.LocalRepoPath,
				Expected: provenance.LocalRepoRevision,
				Current:  repo.Revision,
				Reason:   "revision mismatch",
			})
		}
	}

	for _, source := range provenance.PackageSources {
		name := "packageSource"
		if strings.TrimSpace(source.Component) != "" {
			name = "packageSource:" + source.Component
		}
		info, err := os.Stat(source.Path)
		switch {
		case err != nil:
			changes = append(changes, syncRenderInputChange{
				Name:   name,
				Path:   source.Path,
				Reason: err.Error(),
			})
		case !info.IsDir():
			changes = append(changes, syncRenderInputChange{
				Name:   name,
				Path:   source.Path,
				Reason: "path is not a directory",
			})
		case strings.TrimSpace(source.Digest) == "":
			changes = append(changes, syncRenderInputChange{
				Name:   name,
				Path:   source.Path,
				Reason: "missing package source digest provenance",
			})
		default:
			current, err := hydrate.DigestDirectory(source.Path)
			if err != nil {
				changes = append(changes, syncRenderInputChange{
					Name:     name,
					Path:     source.Path,
					Expected: source.Digest,
					Reason:   err.Error(),
				})
			} else if current.String() != source.Digest {
				changes = append(changes, syncRenderInputChange{
					Name:     name,
					Path:     source.Path,
					Expected: source.Digest,
					Current:  current.String(),
					Reason:   "digest mismatch",
				})
			}
		}
	}

	if len(changes) > 0 {
		status.State = syncRenderInputStateStale
		status.Message = "one or more render inputs differ from recorded provenance; re-rendering may change desired content as well as topology"
		status.ChangedInputs = changes
	}
	return status
}

func syncTopologyRefreshCommand(clusterName string, provenance *hydrate.RenderProvenance) string {
	bomPath := "<bom.yaml>"
	if provenance != nil && strings.TrimSpace(provenance.BOMPath) != "" {
		bomPath = provenance.BOMPath
	}
	args := []string{"sealos", "sync", "render", "--cluster", clusterName}
	switch {
	case provenance != nil && strings.TrimSpace(provenance.ReleaseSource) != "":
		args = append(args,
			"--release-source", provenance.ReleaseSource,
			"--release-line", provenance.DistributionLine,
			"--channel", provenance.ReleaseChannel,
		)
	case provenance != nil && strings.TrimSpace(provenance.ReleaseChannelPath) != "":
		args = append(args, "--release-channel", provenance.ReleaseChannelPath)
	default:
		args = append(args, "--file", bomPath)
	}
	if provenance != nil {
		if strings.TrimSpace(provenance.LocalRepoPath) != "" {
			args = append(args, "--local-repo", provenance.LocalRepoPath)
		}
		if strings.TrimSpace(provenance.LocalPatchRevision) != "" {
			args = append(args, "--local-patch-revision", provenance.LocalPatchRevision)
		}
		for _, source := range provenance.PackageSources {
			if strings.TrimSpace(source.Component) == "" || strings.TrimSpace(source.Path) == "" {
				continue
			}
			args = append(args, "--package-source", source.Component+"="+source.Path)
		}
	}
	return syncShellCommand(args...)
}

func (t *syncExecutionTopology) snapshot() hydrate.ExecutionTopology {
	if t == nil {
		return hydrate.NewSingleNodeExecutionTopology()
	}
	hosts := t.nodeExecutionHosts()
	snapshot := hydrate.ExecutionTopology{
		Source:      "clusterInventory",
		AllNodes:    hosts,
		FirstMaster: strings.TrimSpace(t.firstMaster),
		HostRoles:   make([]hydrate.ExecutionHostRoleList, 0, len(hosts)),
	}
	if t.cluster == nil && len(hosts) == 1 && syncIsLocalExecutionHost(hosts[0]) {
		snapshot.Source = "singleNodeDefault"
	}
	for _, host := range hosts {
		snapshot.HostRoles = append(snapshot.HostRoles, hydrate.ExecutionHostRoleList{
			Host:  host,
			Roles: t.rolesForHost(host),
		})
	}
	return snapshot.Normalize()
}

func changedSyncTopologyFields(a, b hydrate.ExecutionTopology) []string {
	a = a.Normalize()
	b = b.Normalize()
	var changed []string
	if !slices.Equal(a.AllNodes, b.AllNodes) {
		changed = append(changed, "allNodes")
	}
	if strings.TrimSpace(a.FirstMaster) != strings.TrimSpace(b.FirstMaster) {
		changed = append(changed, "firstMaster")
	}
	if !slices.EqualFunc(
		a.HostRoles,
		b.HostRoles,
		func(left, right hydrate.ExecutionHostRoleList) bool {
			return left.Host == right.Host && slices.Equal(left.Roles, right.Roles)
		},
	) {
		changed = append(changed, "hostRoles")
	}
	return changed
}

func defaultSyncExecutionTopology(clusterName string) (*syncExecutionTopology, error) {
	clusterFile := constants.Clusterfile(clusterName)
	if _, err := os.Stat(clusterFile); err != nil {
		if os.IsNotExist(err) {
			return &syncExecutionTopology{
				allNodes:    []string{syncLocalExecutionHost},
				firstMaster: syncLocalExecutionHost,
			}, nil
		}
		return nil, err
	}

	cluster, err := clusterfile.GetClusterFromName(clusterName)
	if err != nil {
		return nil, err
	}
	allNodes := dedupeSyncExecutionHosts(cluster.GetAllIPS())
	if len(allNodes) == 0 {
		return nil, fmt.Errorf("cluster %q has no hosts in Clusterfile inventory", clusterName)
	}
	firstMaster := cluster.GetMaster0IPAndPort()
	if strings.TrimSpace(firstMaster) == "" {
		firstMaster = allNodes[0]
	}
	return &syncExecutionTopology{
		cluster:     cluster,
		allNodes:    allNodes,
		firstMaster: firstMaster,
		hostRoles:   syncHostRolesFromCluster(cluster, allNodes),
	}, nil
}

func defaultSyncRemoteExecutor(topology *syncExecutionTopology) (syncRemoteExecutor, error) {
	if topology == nil || topology.cluster == nil {
		return nil, nil
	}
	return sealosexec.New(ssh.NewCacheClientFromCluster(topology.cluster, true))
}

const syncLocalExecutionHost = "localhost"

func (t *syncExecutionTopology) nodeExecutionHosts() []string {
	if t == nil || len(t.allNodes) == 0 {
		return []string{syncLocalExecutionHost}
	}
	return slices.Clone(t.allNodes)
}

func (t *syncExecutionTopology) hasRemoteHosts() bool {
	for _, host := range t.nodeExecutionHosts() {
		if !syncIsLocalExecutionHost(host) {
			return true
		}
	}
	return false
}

func (t *syncExecutionTopology) hasLocalExecutionHost() bool {
	for _, host := range t.nodeExecutionHosts() {
		if syncIsLocalExecutionHost(host) {
			return true
		}
	}
	return false
}

func (t *syncExecutionTopology) isSingleNode() bool {
	return len(t.nodeExecutionHosts()) <= 1
}

func (t *syncExecutionTopology) rolesForHost(host string) []string {
	if t != nil && len(t.hostRoles) > 0 {
		if roles := syncRolesForExecutionHost(t.hostRoles, host); len(roles) > 0 {
			return roles
		}
	}
	if t == nil || t.cluster == nil {
		if syncIsLocalExecutionHost(host) {
			return []string{v1beta1.MASTER}
		}
		return nil
	}
	roles := t.cluster.GetRolesByIP(host)
	if len(roles) == 0 {
		roles = t.cluster.GetRolesByIP(iputils.GetHostIP(host))
	}
	return slices.Clone(roles)
}

func syncIsControlPlaneHost(topology *syncExecutionTopology, host string) bool {
	for _, role := range topology.rolesForHost(host) {
		if role == v1beta1.MASTER {
			return true
		}
	}
	return false
}

func dedupeSyncExecutionHosts(hosts []string) []string {
	seen := make(map[string]struct{}, len(hosts))
	deduped := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		deduped = append(deduped, host)
	}
	return deduped
}

func syncHostRolesFromCluster(cluster *v1beta1.Cluster, allNodes []string) map[string][]string {
	if cluster == nil {
		return nil
	}
	rolesByHost := make(map[string][]string, len(allNodes))
	for _, host := range allNodes {
		roles := cluster.GetRolesByIP(host)
		if len(roles) == 0 {
			roles = cluster.GetRolesByIP(iputils.GetHostIP(host))
		}
		if len(roles) > 0 {
			rolesByHost[host] = slices.Clone(roles)
		}
	}
	return rolesByHost
}

func syncRolesForExecutionHost(rolesByHost map[string][]string, host string) []string {
	if len(rolesByHost) == 0 {
		return nil
	}
	if roles, ok := rolesByHost[host]; ok {
		return slices.Clone(roles)
	}
	hostIP := iputils.GetHostIP(host)
	if hostIP != "" {
		if roles, ok := rolesByHost[hostIP]; ok {
			return slices.Clone(roles)
		}
	}
	return nil
}

func syncIsLocalExecutionHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || host == syncLocalExecutionHost {
		return true
	}

	hostIP := iputils.GetHostIP(host)
	if hostIP == "" || hostIP == "localhost" || hostIP == "127.0.0.1" || hostIP == "::1" {
		return true
	}

	localAddrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, addr := range localAddrs {
		switch value := addr.(type) {
		case *net.IPNet:
			if value.IP != nil && value.IP.String() == hostIP {
				return true
			}
		case *net.IPAddr:
			if value.IP != nil && value.IP.String() == hostIP {
				return true
			}
		}
	}
	return false
}

func syncCompareBundle(
	clusterName string,
	bundle *hydrate.Bundle,
	bundlePath, kubeconfigPath, hostRoot string,
) (*compare.Result, error) {
	resolver := newSyncKubectlResolver(kubeconfigPath)
	topology, err := syncExecutionTopologyForBundle(clusterName, bundle)
	if err != nil {
		return nil, err
	}
	if topology == nil || syncUseLocalSingleNodeCompare(topology) {
		return compare.CompareBundleWithOptions(
			bundle,
			bundlePath,
			resolver,
			compare.CompareOptions{HostRoot: hostRoot},
		)
	}

	objectResult, err := compare.CompareBundleWithOptions(
		bundle,
		bundlePath,
		resolver,
		compare.CompareOptions{
			HostRoot:      hostRoot,
			SkipHostPaths: true,
		},
	)
	if err != nil {
		return nil, err
	}

	var remoteExec syncRemoteExecutor
	if topology.hasRemoteHosts() {
		remoteExec, err = newSyncRemoteExecutor(topology)
		if err != nil {
			return nil, err
		}
	}

	hostStatuses := make(
		[]compare.HostPathStatus,
		0,
		len(bundle.Spec.TrackedHostPaths)*len(topology.nodeExecutionHosts()),
	)
	for _, host := range topology.nodeExecutionHosts() {
		compareRoot := hostRoot
		cleanup := func() {}
		if syncIsLocalExecutionHost(host) {
			if !topology.hasLocalExecutionHost() {
				continue
			}
		} else {
			compareRoot, cleanup, err = stageSyncRemoteHostRoot(remoteExec, topology, host, bundle.Spec.TrackedHostPaths)
			if err != nil {
				return nil, err
			}
		}
		hostResult, compareErr := compare.CompareBundleWithOptions(
			bundle,
			bundlePath,
			resolver,
			compare.CompareOptions{
				HostRoot:     compareRoot,
				HostIdentity: host,
				SkipObjects:  true,
			},
		)
		cleanup()
		if compareErr != nil {
			return nil, compareErr
		}
		for _, status := range hostResult.HostPaths {
			if !syncTrackedHostPathAppliesToHost(topology, host, status.Tracked) {
				continue
			}
			if syncShouldSuppressMultiNodeGeneratedBootstrapInputHostPath(topology, bundle, status.Tracked) {
				continue
			}
			hostStatuses = append(hostStatuses, status)
		}
	}

	result := &compare.Result{
		Objects:   objectResult.Objects,
		HostPaths: hostStatuses,
	}
	result.Summary = compare.SummarizeResult(result)
	return result, nil
}

func syncPrepareCommitHostRoot(
	clusterName string,
	bundle *hydrate.Bundle,
	hostRoot, selectedHost string,
) (string, func(), error) {
	if strings.TrimSpace(selectedHost) == "" || syncIsLocalExecutionHost(selectedHost) {
		return hostRoot, func() {}, nil
	}
	topology, err := syncExecutionTopologyForBundle(clusterName, bundle)
	if err != nil {
		return "", nil, err
	}
	if topology == nil || topology.isSingleNode() {
		return "", nil, fmt.Errorf("selected host %q requires multi-node topology", selectedHost)
	}
	remoteExec, err := newSyncRemoteExecutor(topology)
	if err != nil {
		return "", nil, err
	}
	tempRoot, err := os.MkdirTemp("", "sealos-sync-commit-hostroot-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tempRoot)
	}
	var trackedPaths []hydrate.TrackedHostPath
	if bundle != nil {
		trackedPaths = bundle.Spec.TrackedHostPaths
	}
	for _, tracked := range trackedPaths {
		if !syncTrackedHostPathAppliesToHost(topology, selectedHost, tracked) {
			continue
		}
		if err := stageSyncRemoteTrackedHostPath(remoteExec, selectedHost, tempRoot, tracked); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return tempRoot, cleanup, nil
}

func syncUseLocalSingleNodeCompare(topology *syncExecutionTopology) bool {
	if topology == nil {
		return true
	}
	hosts := topology.nodeExecutionHosts()
	return len(hosts) <= 1 && len(hosts) > 0 && syncIsLocalExecutionHost(hosts[0])
}

func syncTrackedHostPathAppliesToHost(
	topology *syncExecutionTopology,
	host string,
	tracked hydrate.TrackedHostPath,
) bool {
	if topology == nil {
		return true
	}
	if tracked.ProjectionClass == hydrate.HostPathProjectionClassGenerated ||
		tracked.CompareStrategy == hydrate.HostPathCompareStrategySemanticGenerated ||
		tracked.Generated != nil ||
		tracked.Source == hydrate.InventorySourceGeneratedHook {
		return syncIsControlPlaneHost(topology, host)
	}
	return true
}

func syncShouldSuppressMultiNodeGeneratedBootstrapInputHostPath(
	topology *syncExecutionTopology,
	bundle *hydrate.Bundle,
	tracked hydrate.TrackedHostPath,
) bool {
	if topology == nil || topology.isSingleNode() {
		return false
	}
	if tracked.Source != hydrate.InventorySourceLocalInput ||
		tracked.ProjectionClass != hydrate.HostPathProjectionClassDirect ||
		tracked.CompareStrategy != hydrate.HostPathCompareStrategyBytewiseFile {
		return false
	}
	return tracked.Component == "kubernetes" &&
		tracked.InputName == "kubeadm-cluster-config" &&
		tracked.HostPath == "/etc/kubernetes/kubeadm.yaml" &&
		syncBundleGeneratesKubeadmBootstrapConfig(bundle)
}

func syncBundleGeneratesKubeadmBootstrapConfig(bundle *hydrate.Bundle) bool {
	if bundle == nil {
		return false
	}
	for _, component := range bundle.Spec.Components {
		if component.Name != "kubernetes" && component.PackageName != "kubernetes-rootfs" {
			continue
		}
		hasConfig := false
		hasBootstrapHook := false
		for _, step := range component.Steps {
			if step.Kind == hydrate.StepContent &&
				step.ContentType == packageformat.ContentFile &&
				step.SourcePath == "files/etc/kubernetes/kubeadm.yaml" {
				hasConfig = true
			}
			if step.Kind == hydrate.StepHook &&
				step.HookPhase == packageformat.PhaseBootstrap &&
				step.Target == packageformat.TargetAllNodes {
				hasBootstrapHook = true
			}
		}
		if hasConfig && hasBootstrapHook {
			return true
		}
	}
	return false
}

func stageSyncRemoteHostRoot(
	remoteExec syncRemoteExecutor,
	topology *syncExecutionTopology,
	host string,
	trackedPaths []hydrate.TrackedHostPath,
) (string, func(), error) {
	if remoteExec == nil {
		return "", nil, fmt.Errorf("remote execution client is not configured for host %q", host)
	}
	tempRoot, err := os.MkdirTemp("", "sealos-sync-hostroot-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tempRoot)
	}
	for _, tracked := range trackedPaths {
		if !syncTrackedHostPathAppliesToHost(topology, host, tracked) {
			continue
		}
		if err := stageSyncRemoteTrackedHostPath(remoteExec, host, tempRoot, tracked); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return tempRoot, cleanup, nil
}

func stageSyncRemoteTrackedHostPath(
	remoteExec syncRemoteExecutor,
	host, hostRoot string,
	tracked hydrate.TrackedHostPath,
) error {
	dst, err := resolveSyncHostPath(hostRoot, tracked.HostPath)
	if err != nil {
		return err
	}
	remotePath, err := inspectSyncRemoteTrackedHostPath(remoteExec, host, tracked.HostPath)
	if err != nil {
		return err
	}
	switch remotePath.Kind {
	case "missing":
		return nil
	case "file":
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := prepareSyncStagedHostPath(dst, false); err != nil {
			return err
		}
		if err := remoteExec.Fetch(host, tracked.HostPath, dst); err != nil {
			return err
		}
		if remotePath.Mode != 0 {
			return os.Chmod(dst, remotePath.Mode)
		}
		return nil
	case "symlink":
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := prepareSyncStagedHostPath(dst, false); err != nil {
			return err
		}
		return os.Symlink(remotePath.Target, dst)
	case "dir", "other":
		if err := prepareSyncStagedHostPath(dst, true); err != nil {
			return err
		}
		return os.MkdirAll(dst, 0o755)
	default:
		return fmt.Errorf(
			"unsupported remote host path kind %q for %s on %s",
			remotePath.Kind,
			tracked.HostPath,
			host,
		)
	}
}

func prepareSyncStagedHostPath(path string, wantDir bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if wantDir {
		if info.IsDir() {
			return nil
		}
		return os.Remove(path)
	}
	if info.IsDir() {
		return fmt.Errorf("local staged path %s already exists as a directory", path)
	}
	return os.Remove(path)
}

type syncRemoteTrackedHostPathInfo struct {
	Kind   string
	Target string
	Mode   os.FileMode
}

func inspectSyncRemoteTrackedHostPath(
	remoteExec syncRemoteExecutor,
	host, trackedHostPath string,
) (syncRemoteTrackedHostPathInfo, error) {
	script := strings.Join([]string{
		"path=" + syncShellQuote(trackedHostPath),
		`if [ -L "$path" ]; then`,
		`  printf 'symlink\n'`,
		`  readlink "$path"`,
		`elif [ -f "$path" ]; then`,
		`  printf 'file\n'`,
		`  stat -c '%a' "$path"`,
		`elif [ -d "$path" ]; then`,
		`  printf 'dir\n'`,
		`elif [ -e "$path" ]; then`,
		`  printf 'other\n'`,
		`else`,
		`  printf 'missing\n'`,
		`fi`,
	}, "\n")
	output, err := remoteExec.CmdToString(host, "/bin/bash -lc "+syncShellQuote(script), "\n")
	if err != nil {
		return syncRemoteTrackedHostPathInfo{}, fmt.Errorf(
			"inspect remote host path %s on %s: %w",
			trackedHostPath,
			host,
			err,
		)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return syncRemoteTrackedHostPathInfo{}, fmt.Errorf(
			"inspect remote host path %s on %s returned empty result",
			trackedHostPath,
			host,
		)
	}
	info := syncRemoteTrackedHostPathInfo{Kind: strings.TrimSpace(lines[0])}
	switch info.Kind {
	case "file":
		if len(lines) > 1 {
			mode, err := strconv.ParseUint(strings.TrimSpace(lines[1]), 8, 32)
			if err != nil {
				return syncRemoteTrackedHostPathInfo{}, fmt.Errorf(
					"inspect remote host path %s on %s returned invalid file mode %q: %w",
					trackedHostPath,
					host,
					strings.TrimSpace(lines[1]),
					err,
				)
			}
			info.Mode = os.FileMode(mode)
		}
	case "symlink":
		if len(lines) > 1 {
			info.Target = strings.TrimSpace(lines[1])
		}
	}
	return info, nil
}

func syncShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func syncShellCommand(args ...string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, syncShellQuote(arg))
	}
	return strings.Join(quoted, " ")
}
