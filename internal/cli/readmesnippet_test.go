package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestREADMEGoSnippetRunsFromREADMEBytes(t *testing.T) {
	readme := readREADMEBytes(t)
	code := readmeFence(t, readme, "BEGIN GO SIGNATURE RECIPE", "END GO SIGNATURE RECIPE")
	if !strings.Contains(code, "delivery.SignAll") || !strings.Contains(code, "delivery.VerifyHeader") {
		t.Fatal("Go recipe does not call the exported signing implementation")
	}
	if !strings.Contains(code, "timestamp+301") || !strings.Contains(code, "timestamp-301") || !strings.Contains(
		code,
		"timestamp+300",
	) || !strings.Contains(code, "timestamp-300") {
		t.Fatal("Go recipe omitted one timestamp boundary")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module github.com/cpouldev/pg_noty/readmecheck\n\ngo 1.25.0\n\nrequire github.com/cpouldev/pg_noty v0.0.0\n\nreplace github.com/cpouldev/pg_noty => "+root+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	goSum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), goSum, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", "-mod=mod", ".")
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("README Go recipe failed: %v\n%s", err, output)
	}
}

func TestREADMEUsesTheSharedSignatureVectors(t *testing.T) {
	var vectors struct {
		Body      string   `json:"body"`
		Timestamp int64    `json:"timestamp"`
		Digests   []string `json:"digests"`
	}
	data, err := os.ReadFile(filepath.Join("testdata", "signature_vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(
		data,
		&vectors,
	); err != nil || vectors.Body != "body" || vectors.Timestamp != 1700000000 || len(vectors.Digests) != 2 {
		t.Fatalf("invalid shared vectors: %s", data)
	}
	if !strings.Contains(readREADMEBytes(t), vectors.Digests[0]) && !strings.Contains(
		readREADMEBytes(t),
		"digest(old",
	) {
		t.Fatal("README recipe is not tied to the shared vector shape")
	}
}

func readREADMEBytes(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readmeFence(t *testing.T, readme, begin, end string) string {
	t.Helper()
	start := strings.Index(readme, "<!-- "+begin+" -->")
	finish := strings.Index(readme, "<!-- "+end+" -->")
	if start < 0 || finish <= start {
		t.Fatalf("README fence %s/%s missing", begin, end)
	}
	body := readme[start:finish]
	line := strings.Index(body, "\n")
	if line < 0 {
		t.Fatal("README fence has no code")
	}
	body = body[line+1:]
	if fence := strings.Index(body, "\n```"); fence >= 0 {
		body = body[:fence]
	}
	body = strings.TrimPrefix(body, "```go\n")
	return strings.TrimPrefix(body, "```javascript\n")
}
