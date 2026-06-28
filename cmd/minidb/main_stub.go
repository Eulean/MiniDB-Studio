//go:build !desktop

package main

import "fmt"

// main keeps default test and vet flows desktop-driver free in environments without Fyne's native prerequisites.
func main() {
	fmt.Println("MiniDB Studio desktop build is available with: go run -tags desktop ./cmd/minidb")
}
