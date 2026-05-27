package tests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Thomasparsley/peroapi/internal/converter"
	"github.com/Thomasparsley/peroapi/internal/emitter"
	"github.com/Thomasparsley/peroapi/internal/parser"
)

// generateYAML runs the full pipeline for the given HTTP method convention and returns the
// serialized YAML bytes. Used by golden tests to compare against stored reference files.
func generateYAML(t *testing.T, convention string) []byte {
	t.Helper()

	// Arrange
	schema, err := parser.LoadSchema(fixtureSchema)
	require.NoError(t, err)

	ops, err := parser.LoadOperations(fixtureOps, schema)
	require.NoError(t, err)

	// Act
	conv := converter.New(schema)
	items, err := conv.Convert(ops, convention, "/api/graphql")
	require.NoError(t, err)

	doc := emitter.Build(items, conv.Components(), emitter.Options{Title: "Test API", Version: "1.0.0"})
	dir := t.TempDir()
	out := filepath.Join(dir, "out.yaml")
	require.NoError(t, emitter.Write(doc, out))

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	return data
}

// TestGolden runs the full pipeline for each HTTP method convention ("rest", "post", "get")
// and compares the YAML output byte-for-byte against golden files in tests/golden/.
// Expected: output matches the stored golden file for each convention.
// To regenerate golden files after an intentional output change, run:
//
//	UPDATE_GOLDEN=true go test ./tests/...
func TestGolden(t *testing.T) {
	update := os.Getenv("UPDATE_GOLDEN") == "true"

	conventions := []string{"rest", "post", "get"}
	for _, conv := range conventions {
		conv := conv
		t.Run(conv, func(t *testing.T) {
			// Arrange + Act
			got := generateYAML(t, conv)
			goldenPath := filepath.Join("golden", conv+".yaml")

			if update {
				require.NoError(t, os.MkdirAll("golden", 0o755))
				require.NoError(t, os.WriteFile(goldenPath, got, 0o644))
				t.Logf("updated golden file: %s", goldenPath)
				return
			}

			// Assert
			want, err := os.ReadFile(goldenPath)
			if os.IsNotExist(err) {
				t.Fatalf("golden file %s does not exist; run with UPDATE_GOLDEN=true to create it", goldenPath)
			}
			require.NoError(t, err)

			if !assert.Equal(t, string(want), string(got)) {
				diff := computeDiff(string(want), string(got))
				t.Logf("diff (want → got):\n%s", diff)
			}
		})
	}
}

// computeDiff produces a simple line-level diff for readable test output.
func computeDiff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	var sb strings.Builder
	max := len(wantLines)
	if len(gotLines) > max {
		max = len(gotLines)
	}
	for i := 0; i < max; i++ {
		wl, gl := "", ""
		if i < len(wantLines) {
			wl = wantLines[i]
		}
		if i < len(gotLines) {
			gl = gotLines[i]
		}
		if wl != gl {
			sb.WriteString("- " + wl + "\n")
			sb.WriteString("+ " + gl + "\n")
		}
	}
	return sb.String()
}
