package main

// This file implements fpin's stdin-to-file command.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type temporaryFileCreator func() (string, io.WriteCloser, error)
type destinationFileCreator func(string) (io.WriteCloser, error)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWithCreators(args, stdin, stdout, stderr, createTemporaryFile, createDestinationFile)
}

func runWithCreators(
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	createTemporary temporaryFileCreator,
	createDestination destinationFileCreator,
) int {
	if len(args) > 1 {
		fmt.Fprintln(stderr, "fpin: expected at most one destination file")
		return 1
	}
	if len(args) == 1 && args[0] == "--version" {
		ignoreSIGPIPE()
		if _, err := fmt.Fprintf(stdout, "fpin %s\n", version); err != nil {
			reportError(stderr, "write version", err)
			return 1
		}
		return 0
	}

	temporary := len(args) == 0
	var path string
	var temporaryPath string
	var output io.WriteCloser
	var err error
	if temporary {
		temporaryPath, output, err = createTemporary()
		path = temporaryPath
		if err == nil {
			path, err = filepath.Abs(temporaryPath)
		}
	} else {
		path, err = filepath.Abs(args[0])
		if err == nil {
			output, err = createDestination(path)
		}
	}
	if err != nil {
		if temporary && output != nil {
			output.Close()
			os.Remove(temporaryPath)
		}
		reportError(stderr, "create output file", err)
		return 1
	}

	_, copyErr := io.Copy(output, stdin)
	closeErr := output.Close()
	if copyErr != nil {
		if temporary {
			removeTemporary(path, stderr)
		}
		reportError(stderr, "copy stdin", copyErr)
		if closeErr != nil {
			reportError(stderr, "close output file", closeErr)
		}
		return 1
	}
	if closeErr != nil {
		if temporary {
			removeTemporary(path, stderr)
		}
		reportError(stderr, "close output file", closeErr)
		return 1
	}

	if _, err := fmt.Fprintln(stdout, path); err != nil {
		reportError(stderr, "write output path", err)
		return 1
	}
	return 0
}

func createTemporaryFile() (string, io.WriteCloser, error) {
	file, err := os.CreateTemp("", "fpin-")
	if err != nil {
		return "", nil, err
	}
	return file.Name(), file, nil
}

func createDestinationFile(path string) (io.WriteCloser, error) {
	return os.Create(path)
}

func removeTemporary(path string, stderr io.Writer) {
	if err := os.Remove(path); err != nil {
		reportError(stderr, "remove temporary file", err)
	}
}

func reportError(stderr io.Writer, action string, err error) {
	fmt.Fprintf(stderr, "fpin: %s: %v\n", action, err)
}
