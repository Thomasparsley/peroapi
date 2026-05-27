// Package cmd provides the CLI commands for peroapi.
package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "peroapi",
	Short: "Convert GraphQL persisted operations to OpenAPI v3",
	Long: `peroapi converts a GraphQL persisted operations map (gql-persisted.json)
and a GraphQL schema (schema.graphql) into an OpenAPI v3 YAML specification.

Designed for teams running persisted-only GraphQL APIs with RPC-style endpoints.`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(generateCmd)
}
