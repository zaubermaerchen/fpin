package main

// This file verifies fpin's stdin-to-file command behavior.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProcessPrintsVersion(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		wantVersion string
	}{
		{name: "default", wantVersion: "dev"},
		{name: "injected", version: "v0.1.0", wantVersion: "v0.1.0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			binary := buildFpinWithVersion(t, tc.version)
			cmd := exec.Command(binary, "--version")
			cmd.Stdin = strings.NewReader("stdin must not be read")
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if err != nil {
				t.Fatalf("command failed: %v; stderr = %q", err, stderr.String())
			}
			if got, want := stdout.String(), "fpin "+tc.wantVersion+"\n"; got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestRunVersionDoesNotReadStdinOrCreateOutput(t *testing.T) {
	stdin := &trackingReader{}
	var stdout, stderr bytes.Buffer
	var temporaryCreated, destinationCreated bool
	createTemporary := func() (string, io.WriteCloser, error) {
		temporaryCreated = true
		return "unexpected-temporary", &trackingWriteCloser{}, nil
	}
	createDestination := func(string) (io.WriteCloser, error) {
		destinationCreated = true
		return &trackingWriteCloser{}, nil
	}

	code := runWithCreators([]string{"--version"}, stdin, &stdout, &stderr, createTemporary, createDestination)
	if code != 0 {
		t.Fatalf("runWithCreators() exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got, want := stdout.String(), "fpin dev\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if stdin.read {
		t.Fatal("version command read stdin")
	}
	if temporaryCreated || destinationCreated {
		t.Fatalf("version command created output: temporary=%v destination=%v", temporaryCreated, destinationCreated)
	}
}

func TestProcessReportsOutputClosure(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "js" || runtime.GOOS == "plan9" || runtime.GOOS == "wasip1" {
		t.Skip("closed stdout pipe behavior is Unix-specific")
	}

	tests := []struct {
		name        string
		args        []string
		destination bool
	}{
		{name: "version", args: []string{"--version"}},
		{name: "newline", destination: true},
		{name: "null", args: []string{"-0"}, destination: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			binary := buildFpinWithVersion(t, "")
			args := append([]string{}, tc.args...)
			if tc.destination {
				args = append(args, filepath.Join(t.TempDir(), "output.txt"))
			}
			cmd := exec.Command(binary, args...)
			cmd.Stdin = strings.NewReader("content")
			stdoutRead, stdoutWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := stdoutRead.Close(); err != nil {
				_ = stdoutWrite.Close()
				t.Fatal(err)
			}
			cmd.Stdout = stdoutWrite
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				_ = stdoutWrite.Close()
				t.Fatal(err)
			}
			if err := stdoutWrite.Close(); err != nil {
				t.Fatal(err)
			}

			waitDone := make(chan error, 1)
			go func() {
				waitDone <- cmd.Wait()
			}()
			var waitErr error
			select {
			case waitErr = <-waitDone:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-waitDone
				t.Fatal("timed out waiting for command after stdout closure")
			}
			var exitErr *exec.ExitError
			if !errors.As(waitErr, &exitErr) {
				t.Fatalf("wait error = %v, want exit status 1; stderr = %q", waitErr, stderr.String())
			}
			if got, want := exitErr.ExitCode(), 1; got != want {
				t.Fatalf("exit code = %d, want %d; stderr = %q", got, want, stderr.String())
			}
			if stderr.Len() == 0 {
				t.Fatal("stderr is empty, want output-write diagnostic")
			}
		})
	}
}

func TestRunRejectsVersionWithOtherArgumentsWithoutChangingDestination(t *testing.T) {
	destination := filepath.Join(absoluteTempDir(t), "output.txt")
	if err := os.WriteFile(destination, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("create destination: %v", err)
	}

	for _, args := range [][]string{
		{"--version", destination},
		{destination, "--version"},
		{"--version", "--version"},
	} {
		var stdout, stderr bytes.Buffer
		code := run(args, &trackingReader{}, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("args %q: run() exit code = %d, want 1", args, code)
		}
		if stdout.Len() != 0 {
			t.Fatalf("args %q: stdout = %q, want empty", args, stdout.String())
		}
		if stderr.Len() == 0 {
			t.Fatalf("args %q: stderr is empty, want diagnostic", args)
		}
		if got, err := os.ReadFile(destination); err != nil {
			t.Fatalf("args %q: read destination: %v", args, err)
		} else if want := "keep me"; string(got) != want {
			t.Fatalf("args %q: destination = %q, want %q", args, got, want)
		}
	}
}

func TestRunReportsVersionStdoutFailureWithoutReadingStdinOrCreatingOutput(t *testing.T) {
	stdin := &trackingReader{}
	var stderr bytes.Buffer
	var temporaryCreated, destinationCreated bool
	createTemporary := func() (string, io.WriteCloser, error) {
		temporaryCreated = true
		return "unexpected-temporary", &trackingWriteCloser{}, nil
	}
	createDestination := func(string) (io.WriteCloser, error) {
		destinationCreated = true
		return &trackingWriteCloser{}, nil
	}

	code := runWithCreators(
		[]string{"--version"},
		stdin,
		&errorWriter{err: errors.New("stdout failed")},
		&stderr,
		createTemporary,
		createDestination,
	)
	if code != 1 {
		t.Fatalf("runWithCreators() exit code = %d, want 1", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty, want diagnostic")
	}
	if stdin.read {
		t.Fatal("version command read stdin")
	}
	if temporaryCreated || destinationCreated {
		t.Fatalf("version command created output: temporary=%v destination=%v", temporaryCreated, destinationCreated)
	}
}

func TestRunNullModeWritesNULTerminatedPath(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		destination bool
	}{
		{name: "short alias temporary", args: []string{"-0"}},
		{name: "long alias destination", args: []string{"--null"}, destination: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{}, tc.args...)
			if tc.destination {
				args = append(args, filepath.Join(absoluteTempDir(t), "output.txt"))
			}
			var stdout, stderr bytes.Buffer
			code := run(args, strings.NewReader("null input\n"), &stdout, &stderr)
			if code != 0 {
				t.Fatalf("run() exit code = %d, want 0; stderr = %q", code, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			path := nullTerminatedPath(t, stdout.Bytes())
			t.Cleanup(func() { _ = os.Remove(path) })
			if !filepath.IsAbs(path) {
				t.Fatalf("path = %q, want absolute path", path)
			}
			if got, err := os.ReadFile(path); err != nil {
				t.Fatalf("read output file: %v", err)
			} else if want := "null input\n"; string(got) != want {
				t.Fatalf("output file = %q, want %q", got, want)
			}
		})
	}
}

func TestRunNullModePreservesPathBytes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows filenames cannot contain newlines")
	}

	destination := filepath.Join(absoluteTempDir(t), "output\n.bin")
	t.Cleanup(func() { _ = os.Remove(destination) })
	var stdout, stderr bytes.Buffer
	code := run([]string{"--null", destination}, strings.NewReader("content"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		t.Fatalf("make destination absolute: %v", err)
	}
	want := append([]byte(absolute), 0)
	if !bytes.Equal(stdout.Bytes(), want) {
		t.Fatalf("stdout = %q, want %q", stdout.Bytes(), want)
	}
}

func TestRunAcceptsSeparatorBeforeDashDestination(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "newline", args: []string{"--"}},
		{name: "null", args: []string{"-0", "--"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chdirTest(t, t.TempDir())
			destination := "-fpin-" + strings.ReplaceAll(t.Name(), "/", "-")
			t.Cleanup(func() { _ = os.Remove(destination) })
			args := append(append([]string{}, tc.args...), destination)
			var stdout, stderr bytes.Buffer
			code := run(args, strings.NewReader("dash input"), &stdout, &stderr)
			if code != 0 {
				t.Fatalf("run() exit code = %d, want 0; stderr = %q", code, stderr.String())
			}
			absolute, err := filepath.Abs(destination)
			if err != nil {
				t.Fatalf("make destination absolute: %v", err)
			}
			wantTerminator := byte('\n')
			if tc.name == "null" {
				wantTerminator = 0
			}
			want := append([]byte(absolute), wantTerminator)
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Fatalf("stdout = %q, want %q", stdout.Bytes(), want)
			}
		})
	}
}

func TestRunAcceptsSeparatorBoundariesAndLiteralDestinations(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		destination string
		terminator  byte
	}{
		{name: "separator temporary", args: []string{"--"}, terminator: '\n'},
		{name: "separator null temporary", args: []string{"-0", "--"}, terminator: 0},
		{name: "literal version destination", args: []string{"--"}, destination: "--version", terminator: '\n'},
		{name: "literal null destination", args: []string{"--"}, destination: "-0", terminator: '\n'},
		{name: "literal dash destination", args: []string{"--"}, destination: "-", terminator: '\n'},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chdirTest(t, t.TempDir())
			args := append([]string{}, tc.args...)
			if tc.destination != "" {
				args = append(args, tc.destination)
				t.Cleanup(func() { _ = os.Remove(tc.destination) })
			}

			var stdout, stderr bytes.Buffer
			code := run(args, strings.NewReader("boundary input"), &stdout, &stderr)
			if code != 0 {
				t.Fatalf("run() exit code = %d, want 0; stderr = %q", code, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			if len(stdout.Bytes()) == 0 || stdout.Bytes()[len(stdout.Bytes())-1] != tc.terminator {
				t.Fatalf("stdout = %q, want terminator %d", stdout.Bytes(), tc.terminator)
			}
			path := string(stdout.Bytes()[:len(stdout.Bytes())-1])
			t.Cleanup(func() { _ = os.Remove(path) })
			if !filepath.IsAbs(path) {
				t.Fatalf("path = %q, want absolute path", path)
			}
			if got, err := os.ReadFile(path); err != nil {
				t.Fatalf("read output file: %v", err)
			} else if want := "boundary input"; string(got) != want {
				t.Fatalf("output file = %q, want %q", got, want)
			}
		})
	}
}

func TestRunEmptyFileArgumentUsesDestinationCreator(t *testing.T) {
	stdin := &trackingReader{}
	var stdout, stderr bytes.Buffer
	var temporaryCreated bool
	var destinationPath string
	createTemporary := func() (string, io.WriteCloser, error) {
		temporaryCreated = true
		return "unexpected-temporary", &trackingWriteCloser{}, nil
	}
	createDestination := func(path string) (io.WriteCloser, error) {
		destinationPath = path
		return nil, errors.New("destination create failed")
	}

	code := runWithCreators([]string{""}, stdin, &stdout, &stderr, createTemporary, createDestination)
	if code != 1 {
		t.Fatalf("runWithCreators() exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "create output file") {
		t.Fatalf("stderr = %q, want create diagnostic", stderr.String())
	}
	if stdin.read {
		t.Fatal("empty destination argument read stdin")
	}
	if temporaryCreated {
		t.Fatal("empty destination argument created a temporary file")
	}
	want, err := filepath.Abs("")
	if err != nil {
		t.Fatalf("make expected destination absolute: %v", err)
	}
	if destinationPath != want {
		t.Fatalf("destination creator path = %q, want %q", destinationPath, want)
	}
}

func TestRunReportsShortStdoutWriteAndPreservesOutputContract(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		temporary  bool
		terminator byte
	}{
		{name: "destination newline", args: []string{"output.txt"}, terminator: '\n'},
		{name: "temporary null", args: []string{"-0"}, temporary: true, terminator: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			args := append([]string{}, tc.args...)
			if !tc.temporary {
				args[0] = filepath.Join(t.TempDir(), args[0])
			}
			createTemporary := func() (string, io.WriteCloser, error) {
				file, err := os.CreateTemp(t.TempDir(), "fpin-")
				if err != nil {
					return "", nil, err
				}
				path = file.Name()
				return path, file, nil
			}

			writer := &shortWriter{}
			var stderr bytes.Buffer
			code := runWithCreators(args, strings.NewReader("content"), writer, &stderr, createTemporary, createDestination)
			if code != 1 {
				t.Fatalf("runWithCreators() exit code = %d, want 1", code)
			}
			if stderr.Len() == 0 {
				t.Fatal("stderr is empty, want output-write diagnostic")
			}
			if bytes.HasSuffix(writer.Bytes(), []byte{tc.terminator}) {
				t.Fatalf("stdout = %q, unexpectedly contains success terminator", writer.Bytes())
			}

			if tc.temporary {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("temporary file stat error = %v, want successful temporary to remain", err)
				}
				t.Cleanup(func() { _ = os.Remove(path) })
			} else if got, err := os.ReadFile(args[0]); err != nil {
				t.Fatalf("read destination: %v", err)
			} else if want := "content"; string(got) != want {
				t.Fatalf("destination = %q, want %q", got, want)
			}
		})
	}
}

func TestRunRejectsInvalidArgumentsWithoutTouchingIO(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "unknown long flag", args: []string{"--bogus"}},
		{name: "unknown short flag", args: []string{"-1"}},
		{name: "bare dash", args: []string{"-"}},
		{name: "duplicate short flag", args: []string{"-0", "-0"}},
		{name: "short and long duplicate", args: []string{"-0", "--null"}},
		{name: "long and short duplicate", args: []string{"--null", "-0"}},
		{name: "duplicate long flag", args: []string{"--null", "--null"}},
		{name: "flag after destination", args: []string{"output", "-0"}},
		{name: "long flag after destination", args: []string{"output", "--null"}},
		{name: "too many destinations", args: []string{"output", "extra"}},
		{name: "too many after separator", args: []string{"--", "output", "extra"}},
		{name: "too many null after separator", args: []string{"-0", "--", "output", "extra"}},
		{name: "version with destination", args: []string{"--version", "output"}},
		{name: "destination with version", args: []string{"output", "--version"}},
		{name: "duplicate version", args: []string{"--version", "--version"}},
		{name: "null with version", args: []string{"-0", "--version"}},
		{name: "version with null", args: []string{"--version", "-0"}},
		{name: "version with separator", args: []string{"--version", "--"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdin := &trackingReader{}
			var stdout, stderr bytes.Buffer
			var temporaryCreated, destinationCreated bool
			createTemporary := func() (string, io.WriteCloser, error) {
				temporaryCreated = true
				return "unexpected-temporary", &trackingWriteCloser{}, nil
			}
			createDestination := func(string) (io.WriteCloser, error) {
				destinationCreated = true
				return &trackingWriteCloser{}, nil
			}

			code := runWithCreators(tc.args, stdin, &stdout, &stderr, createTemporary, createDestination)
			if code != 1 {
				t.Fatalf("runWithCreators() exit code = %d, want 1", code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if stderr.Len() == 0 {
				t.Fatal("stderr is empty, want diagnostic")
			}
			if stdin.read {
				t.Fatal("invalid arguments read stdin")
			}
			if temporaryCreated || destinationCreated {
				t.Fatalf("invalid arguments created output: temporary=%v destination=%v", temporaryCreated, destinationCreated)
			}
		})
	}
}

func TestRunNullModeDoesNotPrintPathOnCopyOrCloseFailure(t *testing.T) {
	tests := []struct {
		name      string
		stdin     io.Reader
		wrapFile  func(*os.File) io.WriteCloser
		wantError string
	}{
		{
			name:      "copy",
			stdin:     &errorReader{data: []byte("partial"), err: errors.New("stdin failed")},
			wrapFile:  func(file *os.File) io.WriteCloser { return file },
			wantError: "copy stdin",
		},
		{
			name:      "close",
			stdin:     strings.NewReader("content"),
			wrapFile:  func(file *os.File) io.WriteCloser { return &closeFailingFile{File: file} },
			wantError: "close output file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			createTemporary := func() (string, io.WriteCloser, error) {
				file, err := os.CreateTemp(absoluteTempDir(t), "fpin-")
				if err != nil {
					return "", nil, err
				}
				path = file.Name()
				return path, tc.wrapFile(file), nil
			}

			var stdout, stderr bytes.Buffer
			code := runWithCreators([]string{"-0"}, tc.stdin, &stdout, &stderr, createTemporary, createDestination)
			if code != 1 {
				t.Fatalf("runWithCreators() exit code = %d, want 1", code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.Bytes())
			}
			if !strings.Contains(stderr.String(), tc.wantError) {
				t.Fatalf("stderr = %q, want %q diagnostic", stderr.String(), tc.wantError)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("temporary file stat error = %v, want not-exist", err)
			}
		})
	}
}

func TestRunNullModeReportsStdoutFailureWithoutPrintingPath(t *testing.T) {
	var path string
	createTemporary := func() (string, io.WriteCloser, error) {
		file, err := os.CreateTemp(t.TempDir(), "fpin-")
		if err != nil {
			return "", nil, err
		}
		path = file.Name()
		return path, file, nil
	}
	var stderr bytes.Buffer
	code := runWithCreators(
		[]string{"-0"},
		strings.NewReader("content"),
		&errorWriter{err: errors.New("stdout failed")},
		&stderr,
		createTemporary,
		createDestination,
	)
	if code != 1 {
		t.Fatalf("runWithCreators() exit code = %d, want 1", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty, want diagnostic")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("temporary file stat error = %v, want successful temporary to remain", err)
	}
	if got, err := os.ReadFile(path); err != nil {
		t.Fatalf("read temporary file: %v", err)
	} else if want := "content"; string(got) != want {
		t.Fatalf("temporary file = %q, want %q", got, want)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
}

func TestRunCreatesTemporaryFile(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(nil, strings.NewReader("temporary input\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr = %q", code, stderr.String())
	}

	path := strings.TrimSuffix(stdout.String(), "\n")
	t.Cleanup(func() { os.Remove(path) })
	if !filepath.IsAbs(path) {
		t.Fatalf("temporary path = %q, want absolute path", path)
	}
	temporaryDirectory, err := filepath.Abs(os.TempDir())
	if err != nil {
		t.Fatalf("make temporary directory absolute: %v", err)
	}
	if got, want := filepath.Dir(path), filepath.Clean(temporaryDirectory); got != want {
		t.Fatalf("temporary directory = %q, want %q", got, want)
	}
	if got, want := stdout.String(), path+"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, err := os.ReadFile(path); err != nil {
		t.Fatalf("read temporary file: %v", err)
	} else if want := "temporary input\n"; string(got) != want {
		t.Fatalf("temporary file = %q, want %q", got, want)
	}
}

func TestRunWritesAndOverwritesDestination(t *testing.T) {
	temporaryRoot, err := os.MkdirTemp(".", "fpin-test-")
	if err != nil {
		t.Fatalf("create temporary root: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(temporaryRoot) })
	destination := filepath.Join(filepath.Base(temporaryRoot), "output.txt")
	if err := os.WriteFile(destination, []byte("old content"), 0o600); err != nil {
		t.Fatalf("create destination: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{destination}, strings.NewReader("new content"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr = %q", code, stderr.String())
	}

	absolute, err := filepath.Abs(destination)
	if err != nil {
		t.Fatalf("make destination absolute: %v", err)
	}
	if got, want := stdout.String(), absolute+"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, err := os.ReadFile(destination); err != nil {
		t.Fatalf("read destination: %v", err)
	} else if want := "new content"; string(got) != want {
		t.Fatalf("destination = %q, want %q", got, want)
	}
}

func TestRunDoesNotCanonicalizeDestinationPath(t *testing.T) {
	root := absoluteTempDir(t)
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o755); err != nil {
		t.Fatalf("create real directory: %v", err)
	}
	symlinkDirectory := filepath.Join(root, "link")
	if err := os.Symlink(realDirectory, symlinkDirectory); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	destination := filepath.Join(symlinkDirectory, "output.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{destination}, strings.NewReader("through symlink"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr = %q", code, stderr.String())
	}

	absolute, err := filepath.Abs(destination)
	if err != nil {
		t.Fatalf("make destination absolute: %v", err)
	}
	if got, want := stdout.String(), absolute+"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		t.Fatalf("resolve output symlink: %v", err)
	}
	if canonical == absolute {
		t.Fatalf("output path = %q, unexpectedly canonicalized", absolute)
	}
	if got, err := os.ReadFile(filepath.Join(realDirectory, "output.txt")); err != nil {
		t.Fatalf("read symlink target: %v", err)
	} else if want := "through symlink"; string(got) != want {
		t.Fatalf("symlink target = %q, want %q", got, want)
	}
}

func TestRunRejectsExtraArgumentsWithoutChangingDestination(t *testing.T) {
	destination := filepath.Join(absoluteTempDir(t), "output.txt")
	if err := os.WriteFile(destination, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("create destination: %v", err)
	}
	var stdout, stderr bytes.Buffer

	code := run([]string{destination, "extra"}, strings.NewReader("new content"), &stdout, &stderr)
	if code == 0 {
		t.Fatal("run() exit code = 0, want non-zero")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty, want diagnostic")
	}
	if got, err := os.ReadFile(destination); err != nil {
		t.Fatalf("read destination: %v", err)
	} else if want := "keep me"; string(got) != want {
		t.Fatalf("destination = %q, want %q", got, want)
	}
}

func TestRunDoesNotCreateMissingParentDirectory(t *testing.T) {
	destination := filepath.Join(absoluteTempDir(t), "missing", "output.txt")
	var stdout, stderr bytes.Buffer

	code := run([]string{destination}, strings.NewReader("content"), &stdout, &stderr)
	if code == 0 {
		t.Fatal("run() exit code = 0, want non-zero")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty, want diagnostic")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination stat error = %v, want not-exist", err)
	}
}

func TestRunRemovesTemporaryFileWhenCopyFails(t *testing.T) {
	var path string
	createTemporary := func() (string, io.WriteCloser, error) {
		file, err := os.CreateTemp(absoluteTempDir(t), "fpin-")
		if err != nil {
			return "", nil, err
		}
		path = file.Name()
		return path, file, nil
	}

	var stdout, stderr bytes.Buffer
	code := runWithCreators(
		nil,
		&errorReader{data: []byte("partial data"), err: errors.New("stdin failed")},
		&stdout,
		&stderr,
		createTemporary,
		createDestination,
	)
	if code == 0 {
		t.Fatal("runWithCreators() exit code = 0, want non-zero")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty, want diagnostic")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary file stat error = %v, want not-exist", err)
	}
}

func TestRunRemovesTemporaryFileWhenWriteFails(t *testing.T) {
	var path string
	createTemporary := func() (string, io.WriteCloser, error) {
		file, err := os.CreateTemp(absoluteTempDir(t), "fpin-")
		if err != nil {
			return "", nil, err
		}
		path = file.Name()
		return path, &writeFailingFile{WriteCloser: file}, nil
	}

	var stdout, stderr bytes.Buffer
	code := runWithCreators(
		nil,
		strings.NewReader("content"),
		&stdout,
		&stderr,
		createTemporary,
		createDestination,
	)
	if code == 0 {
		t.Fatal("runWithCreators() exit code = 0, want non-zero")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty, want diagnostic")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary file stat error = %v, want not-exist", err)
	}
}

func TestRunRemovesTemporaryFileWhenCloseFails(t *testing.T) {
	var path string
	createTemporary := func() (string, io.WriteCloser, error) {
		file, err := os.CreateTemp(absoluteTempDir(t), "fpin-")
		if err != nil {
			return "", nil, err
		}
		path = file.Name()
		return path, &closeFailingFile{File: file}, nil
	}

	var stdout, stderr bytes.Buffer
	code := runWithCreators(
		nil,
		strings.NewReader("content"),
		&stdout,
		&stderr,
		createTemporary,
		createDestination,
	)
	if code == 0 {
		t.Fatal("runWithCreators() exit code = 0, want non-zero")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty, want diagnostic")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary file stat error = %v, want not-exist", err)
	}
}

func TestRunRemovesTemporaryFileWhenCopyAndCloseFail(t *testing.T) {
	var path string
	var output *writeAndCloseFailingFile
	createTemporary := func() (string, io.WriteCloser, error) {
		file, err := os.CreateTemp(absoluteTempDir(t), "fpin-")
		if err != nil {
			return "", nil, err
		}
		path = file.Name()
		output = &writeAndCloseFailingFile{WriteCloser: file}
		return path, output, nil
	}

	var stdout, stderr bytes.Buffer
	code := runWithCreators(
		nil,
		strings.NewReader("content"),
		&stdout,
		&stderr,
		createTemporary,
		createDestination,
	)
	if code == 0 {
		t.Fatal("runWithCreators() exit code = 0, want non-zero")
	}
	if !output.closed {
		t.Fatal("temporary file was not closed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary file stat error = %v, want not-exist", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if diagnostics := stderr.String(); !strings.Contains(diagnostics, "copy stdin") {
		t.Fatalf("stderr = %q, want copy diagnostic", diagnostics)
	} else if !strings.Contains(diagnostics, "close output file") {
		t.Fatalf("stderr = %q, want close diagnostic", diagnostics)
	}
}

func TestRunDoesNotRollbackDestinationWhenCopyFails(t *testing.T) {
	destination := filepath.Join(absoluteTempDir(t), "output.txt")
	if err := os.WriteFile(destination, []byte("old content"), 0o600); err != nil {
		t.Fatalf("create destination: %v", err)
	}
	var stdout, stderr bytes.Buffer

	code := runWithCreators(
		[]string{destination},
		&errorReader{data: []byte("partial"), err: errors.New("stdin failed")},
		&stdout,
		&stderr,
		createTemporary,
		createDestination,
	)
	if code == 0 {
		t.Fatal("runWithCreators() exit code = 0, want non-zero")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got, err := os.ReadFile(destination); err != nil {
		t.Fatalf("read destination: %v", err)
	} else if want := "partial"; string(got) != want {
		t.Fatalf("destination = %q, want %q", got, want)
	}
}

func TestRunDoesNotRollbackDestinationWhenCloseFails(t *testing.T) {
	destination := filepath.Join(absoluteTempDir(t), "output.txt")
	if err := os.WriteFile(destination, []byte("old content"), 0o600); err != nil {
		t.Fatalf("create destination: %v", err)
	}
	createDestinationWithCloseFailure := func(path string) (io.WriteCloser, error) {
		file, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		return &closeFailingFile{File: file}, nil
	}
	var stdout, stderr bytes.Buffer

	code := runWithCreators(
		[]string{destination},
		strings.NewReader("new content"),
		&stdout,
		&stderr,
		createTemporary,
		createDestinationWithCloseFailure,
	)
	if code == 0 {
		t.Fatal("runWithCreators() exit code = 0, want non-zero")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got, err := os.ReadFile(destination); err != nil {
		t.Fatalf("read destination: %v", err)
	} else if want := "new content"; string(got) != want {
		t.Fatalf("destination = %q, want %q", got, want)
	}
}

func TestRunReportsStdoutFailure(t *testing.T) {
	destination := filepath.Join(absoluteTempDir(t), "output.txt")
	var stderr bytes.Buffer

	code := run([]string{destination}, strings.NewReader("content"), &errorWriter{err: errors.New("stdout failed")}, &stderr)
	if code == 0 {
		t.Fatal("run() exit code = 0, want non-zero")
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty, want diagnostic")
	}
	if got, err := os.ReadFile(destination); err != nil {
		t.Fatalf("read destination: %v", err)
	} else if want := "content"; string(got) != want {
		t.Fatalf("destination = %q, want %q", got, want)
	}
}

type errorReader struct {
	data []byte
	err  error
}

func (r *errorReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

type writeFailingFile struct {
	io.WriteCloser
}

func (f *writeFailingFile) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

type closeFailingFile struct {
	*os.File
}

func (f *closeFailingFile) Close() error {
	if err := f.File.Close(); err != nil {
		return err
	}
	return errors.New("close failed")
}

type writeAndCloseFailingFile struct {
	io.WriteCloser
	closed bool
}

func (f *writeAndCloseFailingFile) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (f *writeAndCloseFailingFile) Close() error {
	f.closed = true
	if err := f.WriteCloser.Close(); err != nil {
		return err
	}
	return errors.New("close failed")
}

type errorWriter struct {
	err error
}

func (w *errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

type shortWriter struct {
	bytes.Buffer
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	_, _ = w.Buffer.Write(p[:len(p)-1])
	return len(p) - 1, nil
}

func absoluteTempDir(t *testing.T) string {
	t.Helper()
	directory, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatalf("make temp directory absolute: %v", err)
	}
	return directory
}

func chdirTest(t *testing.T, directory string) {
	t.Helper()
	if chdirer, ok := any(t).(interface{ Chdir(string) }); ok {
		chdirer.Chdir(directory)
		return
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(workingDirectory) })
}

func createTemporary() (string, io.WriteCloser, error) {
	file, err := os.CreateTemp("", "fpin-")
	if err != nil {
		return "", nil, err
	}
	return file.Name(), file, nil
}

func createDestination(path string) (io.WriteCloser, error) {
	return os.Create(path)
}

func buildFpinWithVersion(t *testing.T, injected string) string {
	t.Helper()
	binaryName := "fpin"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binary := filepath.Join(t.TempDir(), binaryName)
	args := []string{"build", "-o", binary}
	if injected != "" {
		args = append(args, "-ldflags", "-X main.version="+injected)
	}
	args = append(args, ".")
	command := exec.Command("go", args...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, output)
	}
	return binary
}

type trackingReader struct {
	read bool
}

func (r *trackingReader) Read([]byte) (int, error) {
	r.read = true
	return 0, errors.New("stdin should not be read")
}

type trackingWriteCloser struct {
	bytes.Buffer
}

func (w *trackingWriteCloser) Close() error {
	return nil
}

func nullTerminatedPath(t *testing.T, output []byte) string {
	t.Helper()
	if len(output) == 0 || output[len(output)-1] != 0 {
		t.Fatalf("stdout = %q, want exactly one trailing NUL", output)
	}
	path := output[:len(output)-1]
	if bytes.Contains(path, []byte{0}) {
		t.Fatalf("stdout = %q, want no NUL before terminator", output)
	}
	return string(path)
}
