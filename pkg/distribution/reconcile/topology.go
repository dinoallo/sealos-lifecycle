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

package reconcile

import (
	"context"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"

	"github.com/labring/sealos/pkg/clusterfile"
	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	sealosexec "github.com/labring/sealos/pkg/exec"
	"github.com/labring/sealos/pkg/ssh"
	v1beta1 "github.com/labring/sealos/pkg/types/v1beta1"
	"github.com/labring/sealos/pkg/utils/iputils"
)

const localExecutionHost = "localhost"

type applyRemoteExecutor interface {
	Copy(host, src, dst string) error
	CmdAsyncWithContext(ctx context.Context, host string, cmds ...string) error
	CmdToString(host, cmd, sep string) (string, error)
}

type clusterExecutionTopology struct {
	clusterName   string
	cluster       *v1beta1.Cluster
	allNodes      []string
	firstMaster   string
	hostRoles     map[string][]string
	fallbackLocal bool
}

var loadApplyExecutionTopology = defaultApplyExecutionTopology
var newApplyRemoteExecutor = defaultApplyRemoteExecutor

func applyExecutionTopologyFromSnapshot(clusterName string, snapshot hydrate.ExecutionTopology) (*clusterExecutionTopology, bool, error) {
	if snapshot.Empty() {
		return nil, false, nil
	}
	normalized := snapshot.Normalize()
	if len(normalized.AllNodes) == 0 {
		return nil, true, fmt.Errorf("bundle executionTopology.allNodes cannot be empty")
	}
	if strings.TrimSpace(normalized.FirstMaster) == "" {
		return nil, true, fmt.Errorf("bundle executionTopology.firstMaster cannot be empty")
	}
	return &clusterExecutionTopology{
		clusterName: clusterName,
		allNodes:    normalized.AllNodes,
		firstMaster: normalized.FirstMaster,
		hostRoles:   executionTopologyRolesMap(normalized),
	}, true, nil
}

func defaultApplyExecutionTopology(clusterName string) (*clusterExecutionTopology, error) {
	clusterFile := constants.Clusterfile(clusterName)
	if _, err := os.Stat(clusterFile); err != nil {
		if os.IsNotExist(err) {
			return fallbackLocalExecutionTopology(clusterName), nil
		}
		return nil, err
	}

	cluster, err := clusterfile.GetClusterFromName(clusterName)
	if err != nil {
		return nil, err
	}

	allNodes := dedupeExecutionHosts(cluster.GetAllIPS())
	if len(allNodes) == 0 {
		return nil, fmt.Errorf("cluster %q has no hosts in Clusterfile inventory", clusterName)
	}

	firstMaster := cluster.GetMaster0IPAndPort()
	if strings.TrimSpace(firstMaster) == "" {
		firstMaster = allNodes[0]
	}

	return &clusterExecutionTopology{
		clusterName: clusterName,
		cluster:     cluster,
		allNodes:    allNodes,
		firstMaster: firstMaster,
		hostRoles:   hostRolesFromCluster(cluster, allNodes),
	}, nil
}

func defaultApplyRemoteExecutor(topology *clusterExecutionTopology) (applyRemoteExecutor, error) {
	if topology == nil || topology.cluster == nil {
		return nil, nil
	}
	return sealosexec.New(ssh.NewCacheClientFromCluster(topology.cluster, true))
}

func fallbackLocalExecutionTopology(clusterName string) *clusterExecutionTopology {
	return &clusterExecutionTopology{
		clusterName:   clusterName,
		allNodes:      []string{localExecutionHost},
		firstMaster:   localExecutionHost,
		hostRoles:     map[string][]string{localExecutionHost: []string{v1beta1.MASTER}},
		fallbackLocal: true,
	}
}

func (t *clusterExecutionTopology) nodeExecutionHosts() []string {
	if t == nil || len(t.allNodes) == 0 {
		return []string{localExecutionHost}
	}
	return slices.Clone(t.allNodes)
}

func (t *clusterExecutionTopology) hasRemoteHosts() bool {
	for _, host := range t.nodeExecutionHosts() {
		if !isLocalExecutionHost(host) {
			return true
		}
	}
	return false
}

func (t *clusterExecutionTopology) hasLocalExecutionHost() bool {
	for _, host := range t.nodeExecutionHosts() {
		if isLocalExecutionHost(host) {
			return true
		}
	}
	return false
}

func (t *clusterExecutionTopology) isSingleNode() bool {
	return len(t.nodeExecutionHosts()) <= 1
}

func (t *clusterExecutionTopology) snapshot() hydrate.ExecutionTopology {
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
	if t.fallbackLocal {
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

func (t *clusterExecutionTopology) resolveHostTarget(target packageformat.ExecutionTarget) ([]string, error) {
	switch target {
	case packageformat.TargetAllNodes:
		return t.nodeExecutionHosts(), nil
	case packageformat.TargetFirstMaster:
		if t == nil || strings.TrimSpace(t.firstMaster) == "" {
			return nil, fmt.Errorf("execution target %q cannot be resolved without a first master", target)
		}
		return []string{t.firstMaster}, nil
	case packageformat.TargetCluster:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported execution target %q", target)
	}
}

func (t *clusterExecutionTopology) rolesForHost(host string) []string {
	if t != nil && len(t.hostRoles) > 0 {
		if roles := rolesForExecutionHost(t.hostRoles, host); len(roles) > 0 {
			return roles
		}
	}
	if t == nil || t.cluster == nil {
		if host == localExecutionHost {
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

func (t *clusterExecutionTopology) isFirstMaster(host string) bool {
	if t == nil {
		return host == localExecutionHost
	}
	return iputils.GetHostIP(t.firstMaster) == iputils.GetHostIP(host)
}

func dedupeExecutionHosts(hosts []string) []string {
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

func hostRolesFromCluster(cluster *v1beta1.Cluster, allNodes []string) map[string][]string {
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

func executionTopologyRolesMap(snapshot hydrate.ExecutionTopology) map[string][]string {
	rolesByHost := make(map[string][]string, len(snapshot.HostRoles))
	for _, item := range snapshot.HostRoles {
		host := strings.TrimSpace(item.Host)
		if host == "" {
			continue
		}
		rolesByHost[host] = slices.Clone(item.Roles)
	}
	return rolesByHost
}

func rolesForExecutionHost(rolesByHost map[string][]string, host string) []string {
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

func isLocalExecutionHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || host == localExecutionHost {
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
