package main

// This file verifies fpin's stdin-to-file command behavior.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func absoluteTempDir(t *testing.T) string {
	t.Helper()
	directory, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatalf("make temp directory absolute: %v", err)
	}
	return directory
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
