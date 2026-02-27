// Package main is the entry point for the Whisper load test binary.
// It provides subcommands for different load testing scenarios:
//
//   - saturate: Connection saturation test (LOAD-2)
//   - match:    Matching flow load test (LOAD-3)
//   - chat:     Full chat lifecycle load test (LOAD-4)
//
// Usage:
//
//	loadtest <command> [options]
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "saturate":
		runSaturate(os.Args[2:])
	case "match":
		runMatch(os.Args[2:])
	case "chat":
		runChat(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: loadtest <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  saturate    Connection saturation test — opens N idle connections")
	fmt.Println("  match       Matching flow load test — pairs of users find and accept matches")
	fmt.Println("  chat        Full chat lifecycle load test — connect, match, exchange messages, end")
	fmt.Println()
	fmt.Println("Run 'loadtest <command> -h' for command-specific options.")
}
