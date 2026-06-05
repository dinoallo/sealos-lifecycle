# Production Host Agent Profile

This overlay keeps the current host-agent execution model but makes the
production contract explicit:

- schedule only on control-plane nodes also labeled
  `sealos.io/distribution-controller=true`
- fail the pod before controller startup when required tools are missing from
  the image or host path: `kubectl`, `systemctl`, `tar`, and `sh`
- require the host admin kubeconfig at `/host/etc/kubernetes/admin.conf`
- mark the installed workload with
  `distribution.sealos.io/install-profile=production-host-agent`
- keep RBAC namespace-scoped to `sealos-system`
- use explicit resource requests and limits

The controller container is still privileged and still mounts host state
because rendered bundle apply can write host files and run host tools. Use this
profile only for trusted cluster lifecycle hosts.
