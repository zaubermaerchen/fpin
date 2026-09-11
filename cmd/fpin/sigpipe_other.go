//go:build js || plan9 || wasip1

package main

// This file keeps the SIGPIPE hook portable on targets without SIGPIPE.

// ignoreSIGPIPE is a no-op because these targets do not provide SIGPIPE.
func ignoreSIGPIPE() {}
