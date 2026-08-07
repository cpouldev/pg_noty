//go:build integration

package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestREADMENodeSnippetRunsInPinnedContainerAgainstSharedVectors(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker daemon unavailable")
	}
	if output, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skipf("docker daemon unavailable: %s", output)
	}
	readme := readREADMEBytes(t)
	code := readmeFence(t, readme, "BEGIN NODE SIGNATURE RECIPE", "END NODE SIGNATURE RECIPE")
	if !strings.Contains(code, "timingSafeEqual") || !strings.Contains(code, "301") || !strings.Contains(code, "300") {
		t.Fatal("Node recipe omitted constant-time or boundary checks")
	}
	vectors := readSignatureVectors(t)
	harness := code + "\n" +
		"if (digest('old', 1700000000, 'body') !== '" + vectors.Digests[0] + "') process.exit(1);\n" +
		"if (digest('secret', 1700000000, 'body') !== '" + vectors.Digests[1] + "') process.exit(1);\n"
	command := exec.Command("docker", "run", "--rm", "-i", "node:22.14.0-alpine", "node")
	if output, err := runWithInput(command, []byte(harness)); err != nil {
		t.Fatalf("README Node recipe failed in pinned container: %v\n%s", err, output)
	}
}

type signatureVectors struct {
	Body      string   `json:"body"`
	Timestamp int64    `json:"timestamp"`
	Digests   []string `json:"digests"`
}

func readSignatureVectors(t *testing.T) signatureVectors {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "signature_vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vectors signatureVectors
	if err := json.Unmarshal(data, &vectors); err != nil || vectors.Body != "body" || vectors.Timestamp != 1700000000 || len(vectors.Digests) != 2 {
		t.Fatalf("invalid signature vectors: %s", data)
	}
	return vectors
}

func runWithInput(command *exec.Cmd, input []byte) ([]byte, error) {
	command.Stdin = strings.NewReader(string(input))
	return command.CombinedOutput()
}
