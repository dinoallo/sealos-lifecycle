package main

import (
	"reflect"
	"testing"
)

func TestCompactPathsRemovesNestedEntries(t *testing.T) {
	t.Parallel()

	got := compactPaths([]string{
		"hooks/bootstrap.sh",
		"rootfs/bin/kubelet",
		"files/etc/kubernetes/kubeadm.yaml",
		"package.yaml",
		"rootfs",
		"files/etc/kubernetes/kubeadm.yaml",
	})

	want := []string{
		"package.yaml",
		"rootfs",
		"hooks/bootstrap.sh",
		"files/etc/kubernetes/kubeadm.yaml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("compactPaths() = %v, want %v", got, want)
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()

	if got, want := shellQuote("a'b"), `'a'"'"'b'`; got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}
