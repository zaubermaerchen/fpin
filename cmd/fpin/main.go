package main

// This file implements fpin's stdin-to-file command.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type temporaryFileCreator func() (string, io.WriteCloser, error)
type destinationFileCreator func(string) (io.WriteCloser, error)

type commandOptions struct {
	destination    string
	hasDestination bool
	nullTerminated bool
	version        bool
}

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
	options, err := parseArguments(args)
	if err != nil {
		reportError(stderr, "invalid arguments", err)
		return 1
	}
	if options.version {
		ignoreSIGPIPE()
		if _, err := fmt.Fprintf(stdout, "fpin %s\n", version); err != nil {
			reportError(stderr, "write version", err)
			return 1
		}
		return 0
	}

	temporary := !options.hasDestination
	var path string
	var temporaryPath string
	var output io.WriteCloser
	if temporary {
		temporaryPath, output, err = createTemporary()
		path = temporaryPath
		if err == nil {
			path, err = filepath.Abs(temporaryPath)
		}
	} else {
		path, err = filepath.Abs(options.destination)
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

	ignoreSIGPIPE()
	if err := writeOutputPath(stdout, path, options.nullTerminated); err != nil {
		reportError(stderr, "write output path", err)
		return 1
	}
	return 0
}

func parseArguments(args []string) (commandOptions, error) {
	if len(args) == 1 && args[0] == "--version" {
		return commandOptions{version: true}, nil
	}

	var options commandOptions
	separator := false
	for _, arg := range args {
		if separator {
			if options.hasDestination {
				return commandOptions{}, fmt.Errorf("expected at most one destination file")
			}
			options.destination = arg
			options.hasDestination = true
			continue
		}

		switch arg {
		case "--":
			if options.hasDestination {
				return commandOptions{}, fmt.Errorf("option separator must precede destination file")
			}
			separator = true
		case "-0", "--null":
			if options.nullTerminated {
				return commandOptions{}, fmt.Errorf("null output option specified more than once")
			}
			if options.hasDestination {
				return commandOptions{}, fmt.Errorf("options must precede destination file")
			}
			options.nullTerminated = true
		case "--version":
			return commandOptions{}, fmt.Errorf("--version must be used alone")
		default:
			if strings.HasPrefix(arg, "-") {
				return commandOptions{}, fmt.Errorf("unknown option %q", arg)
			}
			if options.hasDestination {
				return commandOptions{}, fmt.Errorf("expected at most one destination file")
			}
			options.destination = arg
			options.hasDestination = true
		}
	}
	return options, nil
}

func writeOutputPath(stdout io.Writer, path string, nullTerminated bool) error {
	terminator := byte('\n')
	if nullTerminated {
		terminator = 0
	}
	output := make([]byte, len(path)+1)
	copy(output, path)
	output[len(path)] = terminator
	if n, err := stdout.Write(output); err != nil {
		return err
	} else if n != len(output) {
		return io.ErrShortWrite
	}
	return nil
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
