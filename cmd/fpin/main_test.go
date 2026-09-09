package main

import (
	"bytes"
	"testing"
)

func TestWriteHelloWorld(t *testing.T) {
	var buf bytes.Buffer

	writeHelloWorld(&buf)

	if got, want := buf.String(), "Hello World\n"; got != want {
		t.Fatalf("writeHelloWorld() = %q, want %q", got, want)
	}
}
