# codewiki-go-analyzer

`codewiki-go-analyzer` is a lightweight, high-performance static analysis tool written in Go for [CodeWiki](https://github.com/FSoft-AI4Code/CodeWiki). It is designed to parse Go source files and extract structural information, including abstract syntax tree (AST) nodes (structs, interfaces, functions, methods) and call relationships.

## Features

- **Fast Parsing**: Leverages Go's native `go/parser` and `go/ast` packages for robust and speedy analysis.
- **Component Extraction**: Identifies and extracts metadata for:
  - Structs and Interfaces (mapped to "class" components)
  - Functions and Methods
  - Source code segments (including documentation comments)
- **Call Graph Generation**: Extracts function call relationships across the repository (non-test files).
- **JSON Output**: Produces structured JSON output suitable for integration with other tools (e.g., Python parsers).

## Installation

### Prerequisites
- Go 1.25 or higher

### Build
To build the binary, run:

```bash
cd codewiki-go-analyzer
go build -o codewiki-go-analyzer .
```

This will create a `codewiki-go-analyzer` executable in the current directory.

## Usage

`codewiki-go-analyzer` is a command-line tool. The repository should be a Go module (with `go.mod`) or use a `go.work` file at the root.

```bash
./codewiki-go-analyzer -repo <path_to_repo_root>
```

### Arguments

| Flag    | Required | Description                                  |
| ------- | -------- | -------------------------------------------- |
| `-repo` | Yes      | Path to the repository root to analyze.       |

### Example

```bash
./codewiki-go-analyzer -repo .
```

## Output Format

The tool outputs a JSON object to `stdout` containing two main arrays: `nodes` and `call_relationships`.

### Example JSON Output

```json
{
  "nodes": [
    {
      "id": "analyzer.GoAnalyzer",
      "name": "GoAnalyzer",
      "component_type": "class",
      "file_path": "/abs/path/to/go-parser/analyzer/analyzer.go",
      "relative_path": "analyzer/analyzer.go",
      "source_code": "type GoAnalyzer struct { ... }",
      "start_line": 13,
      "end_line": 21,
      "has_docstring": false,
      "node_type": "struct",
      "component_id": "analyzer.GoAnalyzer"
    },
    {
      "id": "analyzer.NewGoAnalyzer",
      "name": "NewGoAnalyzer",
      "component_type": "function",
      "source_code": "func NewGoAnalyzer(...) { ... }",
      "start_line": 23,
      "end_line": 37,
      "parameters": ["filePath", "repoPath"]
    }
  ],
  "call_relationships": [
    {
      "caller": "analyzer.NewGoAnalyzer",
      "callee": "os.ReadFile",
      "call_line": 24,
      "is_resolved": true,
      "relationship_type": "calls"
    }
  ]
}
```

## Integration with CodeWiki

In the CodeWiki Python backend, `codewiki-go-analyzer` is invoked via `subprocess`. The wrapper implementation can be found in `codewiki/src/be/dependency_analyzer/analyzers/go.py`.

It treats `codewiki-go-analyzer` as a black-box parser:
1. Python detects a Go repository.
2. Python executes `codewiki-go-analyzer` with the repository root.
3. `codewiki-go-analyzer` analyzes the file and prints JSON to stdout.
4. Python captures stdout, deserializes the JSON, and integrates the nodes into the global dependency graph.

## Development

### Module Structure

- `main.go`: Entry point. Handles CLI flag parsing and JSON output marshaling.
- `analyzer/`: Core logic for AST traversal and extraction.
  - `analyzer.go`: `GoAnalyzer` struct and visitor methods (`visitTypeSpec`, `visitFuncDecl`).
- `models/`: Go struct definitions for the output JSON format (`Node`, `CallRelationship`).

### Running Tests

```bash
go test -v ./analyzer
```
