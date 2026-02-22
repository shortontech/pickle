package main

import (
	"fmt"
	"os"

	"github.com/pickle-framework/pickle/pkg/tickler"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: tickle <project_dir> <package_name>\n")
		os.Exit(1)
	}

	projectDir := os.Args[1]
	packageName := os.Args[2]
	cookedDir := "pkg/cooked"
	outputPath := projectDir + "/generated/pickle.go"

	if err := tickler.TickleToFile(cookedDir, packageName, outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "tickle failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("tickled → %s\n", outputPath)
}
