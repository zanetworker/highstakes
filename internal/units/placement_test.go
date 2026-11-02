package units

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestPlacements(t *testing.T) {
	fs := fstest.MapFS{
		"deploy/docker/Dockerfile.sandbox":      file("FROM scratch\nCOPY x /openshell-sandbox\nENTRYPOINT [\"/openshell-sandbox\"]\n"),
		"deploy/docker/Dockerfile.gateway":      file("FROM base\nentrypoint /usr/local/bin/openshell-gateway --port 8080\nCMD [\"--bind\", \"0.0.0.0\"]\n"),
		"deploy/docker/Dockerfile.shell":        file("FROM base\nCMD [\"sh\", \"-c\", \"openshell-sandbox\"]\n"),
		"deploy/helm/os/Chart.yaml":             file("name: os\n"),
		"deploy/helm/os/values.yaml":            file("gateway:\n  image:\n    repository: openshell/gateway\n\nsandboxRuntime:\n  image:\n    repository: \"openshell/sandbox\"\nbase:\n  image: nvcr.io/nvidia/base/ubuntu\n"),
		"deploy/helm/os/templates/sts.yaml":     file("      image: {{ .Values.gateway.image.repository }}\n"),
		"deploy/kube/pod.yaml":                  file("kind: Pod\nspec:\n  containers:\n    - name: sup\n      image: ghcr.io/acme/openshell-supervisor:1.2@sha256:abc\n"),
		"node_modules/x/Dockerfile":             file("ENTRYPOINT [\"/openshell-sandbox\"]\n"),
		"tests/k3s/play.yaml":                   file("image: openshell/gateway\n"), // test fixtures are not deployments
		"deploy/docker/Dockerfile.dockerignore": file("ENTRYPOINT [\"/openshell-sandbox\"]\n"),
	}
	names := []string{"openshell-gateway", "openshell-sandbox", "openshell-supervisor", "openshell-sandbox-backend"}
	got, err := Placements(fs, []string{"node_modules"}, names)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]types.Placement{
		"openshell-gateway": {
			{Kind: "dockerfile", Detail: "ENTRYPOINT of deploy/docker/Dockerfile.gateway", Evidence: "deploy/docker/Dockerfile.gateway:2"},
			{Kind: "helm", Detail: "helm chart deploy/helm/os: gateway image openshell/gateway", Evidence: "deploy/helm/os/values.yaml:3"},
		},
		"openshell-sandbox": {
			{Kind: "dockerfile", Detail: "ENTRYPOINT of deploy/docker/Dockerfile.sandbox", Evidence: "deploy/docker/Dockerfile.sandbox:3"},
			{Kind: "helm", Detail: "helm chart deploy/helm/os: sandboxRuntime image openshell/sandbox", Evidence: "deploy/helm/os/values.yaml:7"},
		},
		"openshell-supervisor": {
			{Kind: "k8s", Detail: "image ghcr.io/acme/openshell-supervisor:1.2@sha256:abc in deploy/kube/pod.yaml", Evidence: "deploy/kube/pod.yaml:5"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Placements =\n%+v\nwant\n%+v", got, want)
	}
}

func TestImageName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"openshell/sandbox", "sandbox"},
		{"ghcr.io/acme/app:1.2", "app"},
		{"localhost:5000/app@sha256:abc", "app"},
		{"app", "app"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := imageName(tt.in); got != tt.want {
				t.Errorf("imageName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPlacements_NoUnits(t *testing.T) {
	got, err := Placements(fstest.MapFS{"Dockerfile": file("ENTRYPOINT [\"/x\"]\n")}, nil, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("Placements = %v, %v; want empty", got, err)
	}
}
