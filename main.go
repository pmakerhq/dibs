package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:           "dibs <service>",
		Short:         "Session-scoped port allocator",
		Version:       version,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(args[0])
		},
	}

	getCmd := &cobra.Command{
		Use:   "get <service>",
		Short: "Get (or reuse) this session's port for <service>",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(args[0])
		},
	}

	var releaseAll bool
	releaseCmd := &cobra.Command{
		Use:   "release [service]",
		Short: "Release this session's port for <service> (or all, with --all)",
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
	releaseCmd.Flags().BoolVar(&releaseAll, "all", false, "release every port held by this session")

	var listJSON bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List live allocations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(listJSON)
		},
	}
	listCmd.Flags().BoolVar(&listJSON, "json", false, "output as JSON")

	envCmd := &cobra.Command{
		Use:   "env <service...>",
		Short: "Print PORT env vars for one or more services, e.g. eval $(dibs env postgresql)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnv(args)
		},
	}

	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check dibs' on-disk state and config for issues",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdDoctor()
		},
	}

	rootCmd.AddCommand(getCmd, releaseCmd, listCmd, envCmd, doctorCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "dibs:", err)
		os.Exit(1)
	}
}

func runGet(service string) error {
	port, err := cmdGet(service)
	if err != nil {
		return err
	}
	fmt.Println(port)
	return nil
}

func runRelease(service string) error {
	return cmdRelease(service)
}

func runList(asJSON bool) error {
	entries, err := cmdList()
	if err != nil {
		return err
	}
	if asJSON {
		if entries == nil {
			entries = []Entry{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}
	if len(entries) == 0 {
		fmt.Println("no live allocations")
		return nil
	}
	for _, e := range entries {
		fmt.Printf("%-12s %-6d session=%s pid=%d allocated=%s\n", e.Service, e.Port, e.SessionKey, e.PID, e.AllocatedAt)
	}
	return nil
}

// envVarName turns a service name into an env var name, e.g.
// "postgresql" -> "POSTGRESQL_PORT", "my-service" -> "MY_SERVICE_PORT".
func envVarName(service string) string {
	return strings.ToUpper(strings.ReplaceAll(service, "-", "_")) + "_PORT"
}

func runEnv(services []string) error {
	for _, service := range services {
		port, err := cmdGet(service)
		if err != nil {
			return err
		}
		fmt.Printf("export %s=%d\n", envVarName(service), port)
	}
	return nil
}
