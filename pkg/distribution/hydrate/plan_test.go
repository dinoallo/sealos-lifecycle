package hydrate

import (
	"fmt"
	"testing"

	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/packageformat"
)

func TestBuildPlan(t *testing.T) {
	t.Parallel()

	doc := testBOM()
	plan, err := BuildPlan(doc, packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
		switch image {
		case "registry.example.io/sealos/calico:3.28.0@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb":
			return calicoPackage(), nil
		case "registry.example.io/sealos/ingress-nginx:1.10.1@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc":
			return ingressPackage(), nil
		default:
			return nil, fmt.Errorf("unexpected image %q", image)
		}
	}))
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}

	if got, want := len(plan.Components), 2; got != want {
		t.Fatalf("len(plan.Components) = %d, want %d", got, want)
	}
	if got, want := plan.Components[0].Name, "calico"; got != want {
		t.Fatalf("plan.Components[0].Name = %q, want %q", got, want)
	}
	if got, want := plan.Components[1].Name, "ingress-nginx"; got != want {
		t.Fatalf("plan.Components[1].Name = %q, want %q", got, want)
	}
	if got, want := len(plan.Components[0].Steps), 3; got != want {
		t.Fatalf("len(plan.Components[0].Steps) = %d, want %d", got, want)
	}
	if got, want := plan.Components[0].Steps[0].ContentType, packageformat.ContentFile; got != want {
		t.Fatalf("first calico step contentType = %q, want %q", got, want)
	}
	if got, want := plan.Components[0].Steps[2].Kind, StepHook; got != want {
		t.Fatalf("last calico step kind = %q, want %q", got, want)
	}
}

func TestBuildPlanRejectsMissingRequiredPackageDependency(t *testing.T) {
	t.Parallel()

	doc := testBOM()
	_, err := BuildPlan(doc, packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
		switch image {
		case "registry.example.io/sealos/calico:3.28.0@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb":
			return calicoPackage(), nil
		case "registry.example.io/sealos/ingress-nginx:1.10.1@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc":
			pkg := ingressPackage()
			pkg.Spec.Dependencies = []packageformat.Dependency{
				{Name: "cert-manager"},
			}
			return pkg, nil
		default:
			return nil, fmt.Errorf("unexpected image %q", image)
		}
	}))
	if err == nil {
		t.Fatal("BuildPlan() error = nil, want error")
	}
}

func TestBuildPlanRejectsDependencyCycle(t *testing.T) {
	t.Parallel()

	doc := testBOM()
	doc.Spec.Components[0].Dependencies = []string{"ingress-nginx"}

	_, err := BuildPlan(doc, packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
		switch image {
		case "registry.example.io/sealos/calico:3.28.0@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb":
			return calicoPackage(), nil
		case "registry.example.io/sealos/ingress-nginx:1.10.1@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc":
			return ingressPackage(), nil
		default:
			return nil, fmt.Errorf("unexpected image %q", image)
		}
	}))
	if err == nil {
		t.Fatal("BuildPlan() error = nil, want error")
	}
}

func testBOM() *bom.BOM {
	doc := bom.New("default-platform", "rev-20240423", bom.ChannelBeta)
	doc.Spec.Components = []bom.Component{
		{
			Name:    "calico",
			Kind:    "infra",
			Version: "3.28.0",
			Artifact: bom.ArtifactReference{
				Name:   "calico-artifact",
				Image:  "registry.example.io/sealos/calico:3.28.0",
				Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
		},
		{
			Name:         "ingress-nginx",
			Kind:         "infra",
			Version:      "1.10.1",
			Dependencies: []string{"calico"},
			Artifact: bom.ArtifactReference{
				Name:   "ingress-artifact",
				Image:  "registry.example.io/sealos/ingress-nginx:1.10.1",
				Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			},
		},
	}
	return doc
}

func calicoPackage() *packageformat.ComponentPackage {
	doc := packageformat.New("calico-package", "calico", "3.28.0", packageformat.ClassApplication)
	doc.Spec.Inputs = []packageformat.Input{
		{Name: "env", Type: packageformat.InputEnv, Path: "env/CALICO_BACKEND"},
	}
	doc.Spec.Contents = []packageformat.Content{
		{Name: "calico-values", Type: packageformat.ContentFile, Path: "files/calico-values.yaml"},
		{Name: "calico-manifests", Type: packageformat.ContentManifest, Path: "manifests/calico.yaml"},
	}
	doc.Spec.Hooks = []packageformat.Hook{
		{Name: "healthcheck", Phase: packageformat.PhaseHealth, Target: packageformat.TargetCluster, Path: "hooks/health.sh"},
	}
	return doc
}

func ingressPackage() *packageformat.ComponentPackage {
	doc := packageformat.New("ingress-package", "ingress-nginx", "1.10.1", packageformat.ClassApplication)
	doc.Spec.Dependencies = []packageformat.Dependency{
		{Name: "calico", Version: "3.28.0"},
	}
	doc.Spec.Contents = []packageformat.Content{
		{Name: "ingress-manifests", Type: packageformat.ContentManifest, Path: "manifests/ingress.yaml"},
	}
	return doc
}
