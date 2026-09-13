package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:           "dibs <service>",
		Short:         "Project-scoped port allocator",
		Version:       version,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return runGet(cmd.OutOrStdout(), args[0])
		},
	}

	getCmd := &cobra.Command{
		Use:   "get <service>",
		Short: "Get (or reuse) this project's port for <service>",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd.OutOrStdout(), args[0])
		},
	}

	var releaseAll bool
	releaseCmd := &cobra.Command{
		Use:   "release [service]",
		Short: "Release this project's port for <service> (or all, with --all)",
		Args: func(cmd *cobra.Command, args []string) error {
			if releaseAll {
				return cobra.NoArgs(cmd, args)
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if releaseAll {
				return cmdReleaseAll()
			}
			return cmdRelease(args[0])
		},
	}
	releaseCmd.Flags().BoolVar(&releaseAll, "all", false, "release every port held by this project")

	var listJSON bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List live allocations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.OutOrStdout(), listJSON)
		},
	}
	listCmd.Flags().BoolVar(&listJSON, "json", false, "output as JSON")

	envCmd := &cobra.Command{
		Use:   "env <service...>",
		Short: "Print PORT env vars for one or more services, e.g. eval $(dibs env postgresql)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnv(cmd.OutOrStdout(), args)
		},
	}

	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check dibs' on-disk state and config for issues",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdDoctor(cmd.OutOrStdout())
		},
	}

	rootCmd.AddCommand(getCmd, releaseCmd, listCmd, envCmd, doctorCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "dibs:", err)
		os.Exit(1)
	}
}

func runGet(w io.Writer, service string) error {
	port, err := cmdGet(service)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, port)
	return nil
}

func runList(w io.Writer, asJSON bool) error {
	entries, err := cmdList()
	if err != nil {
		return err
	}
	if asJSON {
		if entries == nil {
			entries = []Entry{}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}
	if len(entries) == 0 {
		fmt.Fprintln(w, "no live allocations")
		return nil
	}
	for _, e := range entries {
		fmt.Fprintf(w, "%-12s %-6d %s allocated=%s\n", e.Service, e.Port, e.Project, e.AllocatedAt)
	}
	return nil
}

// envVarName turns a service name into a shell-safe env var name, e.g.
// "postgresql" -> "POSTGRESQL_PORT", "my-service" -> "MY_SERVICE_PORT".
// Anything that isn't alphanumeric becomes "_", and a leading digit gets an
// "_" prefix, so `eval $(dibs env ...)` can't emit an unassignable name for a
// service like "my.service" or "3scale".
func envVarName(service string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		}
		return '_'
	}, service)
	if safe != "" && safe[0] >= '0' && safe[0] <= '9' {
		safe = "_" + safe
	}
	return strings.ToUpper(safe) + "_PORT"
}

func runEnv(w io.Writer, services []string) error {
	for _, service := range services {
		port, err := cmdGet(service)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "export %s=%d\n", envVarName(service), port)
	}
	return nil
}
