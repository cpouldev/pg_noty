//go:build integration

package cli

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestImagePropertiesFromBuiltArtefact(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker daemon unavailable")
	}
	if output, err := exec.Command("docker", "buildx", "ls").CombinedOutput(); err != nil || !strings.Contains(string(output), "linux/amd64") || !strings.Contains(string(output), "linux/arm64") {
		t.Skip("docker buildx lacks both required platforms")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion := moduleGoVersion(t, root)
	for _, platform := range []string{"linux/amd64", "linux/arm64"} {
		t.Run(platform, func(t *testing.T) {
			image := "pgnoty-image-test-" + strings.ReplaceAll(platform, "/", "-")
			build := exec.Command("docker", "buildx", "build", "--load", "--platform", platform,
				"--build-arg", "GO_VERSION="+goVersion, "-t", image, root)
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("image build failed: %v\n%s", err, output)
			}
			inspectImage(t, image, platform)
		})
	}
}

func moduleGoVersion(t *testing.T, root string) string {
	t.Helper()
	command := exec.Command("go", "mod", "edit", "-json")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var module struct {
		Go string `json:"Go"`
	}
	if err := json.Unmarshal(output, &module); err != nil || module.Go == "" {
		t.Fatalf("go.mod did not declare a toolchain version: %s", output)
	}
	return module.Go
}

func inspectImage(t *testing.T, image, platform string) {
	t.Helper()
	output, err := exec.Command("docker", "image", "inspect", image).Output()
	if err != nil {
		t.Fatal(err)
	}
	var images []struct {
		Size   int64 `json:"Size"`
		Config struct {
			User       string   `json:"User"`
			Entrypoint []string `json:"Entrypoint"`
		} `json:"Config"`
	}
	if err := json.Unmarshal(output, &images); err != nil || len(images) != 1 {
		t.Fatalf("invalid image inspection: %s", output)
	}
	imageInfo := images[0]
	if imageInfo.Size >= 10*1024*1024 || imageInfo.Config.User == "" || imageInfo.Config.User == "0" {
		t.Fatalf("image size/user = %d/%q, want under 10 MB and non-root", imageInfo.Size, imageInfo.Config.User)
	}
	wantEntrypoint := []string{"/pg_noty", "run", "-f", "/etc/pg_noty/listeners.yaml"}
	if fmt.Sprint(imageInfo.Config.Entrypoint) != fmt.Sprint(wantEntrypoint) {
		t.Fatalf("entrypoint=%v, want %v", imageInfo.Config.Entrypoint, wantEntrypoint)
	}
	if output, err := exec.Command("docker", "run", "--rm", "--platform", platform, image, "--help").CombinedOutput(); err != nil || !strings.Contains(string(output), "Usage:") {
		t.Fatalf("running %s image failed: %v\n%s", platform, err, output)
	}
}
