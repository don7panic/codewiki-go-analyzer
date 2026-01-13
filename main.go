package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/don7panic/codewiki-go-analyzer/analyzer"
	"github.com/don7panic/codewiki-go-analyzer/models"
)

func main() {
	filePath := flag.String("file", "", "Path to the Go file to analyze")
	repoPath := flag.String("repo", "", "Path to the repository root")
	flag.Parse()

	if *filePath == "" {
		fmt.Println("Error: --file argument is required")
		os.Exit(1)
	}

	an, err := analyzer.NewGoAnalyzer(*filePath, *repoPath)
	if err != nil {
		fmt.Printf("Error creating analyzer: %v\n", err)
		os.Exit(1)
	}

	if err := an.Analyze(); err != nil {
		fmt.Printf("Error analyzing file: %v\n", err)
		os.Exit(1)
	}

	result := models.AnalysisResult{
		Nodes:             an.Nodes,
		CallRelationships: an.Relationships,
	}

	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling output: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(output))
}
