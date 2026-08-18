package tests_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// generateYAML runs the shared pipeline for the given HTTP method convention and returns the
// serialized YAML bytes used by the golden comparison.
func generateYAML(t *testing.T, convention string) []byte {
	t.Helper()
	data, err := yaml.Marshal(runPipeline(t, convention))
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

			// Assert — assert.Equal prints a unified diff on mismatch.
			want, err := os.ReadFile(goldenPath)
			if os.IsNotExist(err) {
				t.Fatalf("golden file %s does not exist; run with UPDATE_GOLDEN=true to create it", goldenPath)
			}
			require.NoError(t, err)
			assert.Equal(t, string(want), string(got))
		})
	}
}
