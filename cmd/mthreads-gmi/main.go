package main

import (
	"fmt"
	"os"

	"github.com/chaunceyjiang/fake-gpu/pkg/mthreads"
)

func main() {
	if err := mthreads.RootCmd.Execute(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
