package main

import (
	"fmt"
	"os"

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

	releaseCmd := &cobra.Command{
		Use:   "release <service>",
		Short: "Release this session's port for <service>",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRelease(args[0])
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List live allocations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList()
		},
	}

	rootCmd.AddCommand(getCmd, releaseCmd, listCmd)

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

func runList() error {
	entries, err := cmdList()
	if err != nil {
		return err
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
