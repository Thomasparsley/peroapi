package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Thomasparsley/peroapi/internal/converter"
	"github.com/Thomasparsley/peroapi/internal/emitter"
	"github.com/Thomasparsley/peroapi/internal/parser"
)

// affirmativeAnswers are the accepted yes-responses for interactive prompts.
var affirmativeAnswers = map[string]bool{
	"":    true,
	"y":   true,
	"yes": true,
}

// outputTarget describes where the generated YAML should be written.
type outputTarget struct {
	// Path is the file path to write to. Empty when UseStdout is true.
	Path      string
	UseStdout bool
}

var (
	flagSchema     string
	flagOperations string
	flagOutput     string
	flagMethod     string
	flagBasePath   string
	flagTitle      string
	flagVersion    string
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate an OpenAPI v3 spec from a GraphQL schema and persisted operations",
	RunE:  runGenerate,
}

func init() {
	generateCmd.Flags().StringVarP(&flagSchema, "schema", "s", "", "Path to schema.graphql (required)")
	generateCmd.Flags().StringVarP(&flagOperations, "operations", "o", "", "Path to gql-persisted.json (required)")
	generateCmd.Flags().StringVar(&flagOutput, "output", "", "Path to write the OpenAPI YAML output (default: prompt)")
	generateCmd.Flags().StringVar(&flagMethod, "method-convention", "rest", `HTTP method strategy: "rest" (GET queries/POST mutations), "post", or "get"`)
	generateCmd.Flags().StringVar(&flagBasePath, "base-path", "/api/graphql", "Base path prefix for all routes")
	generateCmd.Flags().StringVar(&flagTitle, "title", "Generated API", "OpenAPI info.title")
	generateCmd.Flags().StringVar(&flagVersion, "version", "1.0.0", "OpenAPI info.version")

	_ = generateCmd.MarkFlagRequired("schema")
	_ = generateCmd.MarkFlagRequired("operations")
}

// runGenerate orchestrates the full pipeline: parse schema → load operations → convert to PathItems → emit YAML.
func runGenerate(cmd *cobra.Command, _ []string) error {
	schema, err := parser.LoadSchema(flagSchema)
	if err != nil {
		return fmt.Errorf("loading schema: %w", err)
	}

	ops, err := parser.LoadOperations(flagOperations, schema)
	if err != nil {
		return fmt.Errorf("loading operations: %w", err)
	}

	conv := converter.New(schema)
	converted, err := conv.Convert(ops, flagMethod, flagBasePath)
	if err != nil {
		return fmt.Errorf("converting operations: %w", err)
	}

	doc := emitter.Build(converted, conv.Components(), emitter.Options{
		Title:   flagTitle,
		Version: flagVersion,
	})

	target := outputTarget{Path: flagOutput}
	if flagOutput == "" {
		target, err = resolveOutput(cmd)
		if err != nil {
			return err
		}
	}

	writePath := ""
	if !target.UseStdout {
		writePath = target.Path
	}
	return emitter.Write(doc, writePath)
}

// resolveOutput prompts the user when --output is not provided.
func resolveOutput(cmd *cobra.Command) (outputTarget, error) {
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprint(out, "No output file specified. Print to stdout? [Y/n]: "); err != nil {
		return outputTarget{}, fmt.Errorf("writing prompt: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return outputTarget{}, fmt.Errorf("reading input: %w", err)
	}

	if affirmativeAnswers[strings.TrimSpace(strings.ToLower(answer))] {
		return outputTarget{UseStdout: true}, nil
	}

	if _, err := fmt.Fprint(out, "Output filename: "); err != nil {
		return outputTarget{}, fmt.Errorf("writing prompt: %w", err)
	}

	filename, err := reader.ReadString('\n')
	if err != nil {
		return outputTarget{}, fmt.Errorf("reading filename: %w", err)
	}
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return outputTarget{}, fmt.Errorf("no output filename provided")
	}
	return outputTarget{Path: filename}, nil
}
