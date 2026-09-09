package main

import (
	"fmt"
	"io"
	"os"
)

func writeHelloWorld(w io.Writer) {
	fmt.Fprintln(w, "Hello World")
}

func main() {
	writeHelloWorld(os.Stdout)
}
