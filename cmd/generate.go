package cmd

import (
	"bufio"
	"fmt"
	"io"
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
	if err := converter.ValidateConvention(flagMethod); err != nil {
		return err
	}

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

	for _, w := range conv.Warnings() {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning:", w)
	}

	doc := emitter.Build(converted, conv.Components(), emitter.Options{
		Title:   flagTitle,
		Version: flagVersion,
	})

	outputPath := flagOutput
	if outputPath == "" {
		outputPath, err = resolveOutputPath(cmd)
		if err != nil {
			return err
		}
	}
	return emitter.Write(doc, outputPath)
}

// resolveOutputPath interactively asks for an output destination when --output
// is not provided. An empty return value means stdout. Prompts are written to
// stderr so they never contaminate the YAML document when it is emitted to stdout.
func resolveOutputPath(cmd *cobra.Command) (string, error) {
	out := cmd.ErrOrStderr()
	reader := bufio.NewReader(os.Stdin)

	answer, err := prompt(out, reader, "No output file specified. Print to stdout? [Y/n]: ")
	if err != nil {
		return "", err
	}
	if affirmativeAnswers[strings.ToLower(answer)] {
		return "", nil
	}

	filename, err := prompt(out, reader, "Output filename: ")
	if err != nil {
		return "", err
	}
	if filename == "" {
		return "", fmt.Errorf("no output filename provided")
	}
	return filename, nil
}

// prompt writes msg and reads one line from reader, trimmed of whitespace.
func prompt(out io.Writer, reader *bufio.Reader, msg string) (string, error) {
	if _, err := fmt.Fprint(out, msg); err != nil {
		return "", fmt.Errorf("writing prompt: %w", err)
	}
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return strings.TrimSpace(line), nil
}
