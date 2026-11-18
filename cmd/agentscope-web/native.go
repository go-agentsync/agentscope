// Native stub: the real program is the wasm canvas driver in main.go. This
// exists so `go build ./...` links off-wasm.
//
//go:build !(js && wasm)

package main

import "fmt"

func banner() string {
	return "agentscope-web is the browser half of agentscope (wasm).\n" +
		"`agentscope serve` builds it on first run."
}

func main() { fmt.Println(banner()) }
