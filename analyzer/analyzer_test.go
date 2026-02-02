package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGoMod(t *testing.T, dir string) {
	t.Helper()
	content := "module example.com/test\n\ngo 1.25\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}
}

func TestAnalyzeStructSourceExtraction(t *testing.T) {
	// 1. Setup temporary test file
	content := `package testpkg

// MyStruct is a test struct
type MyStruct struct {
	Field1 string
}
`
	tmpDir := t.TempDir()
	writeGoMod(t, tmpDir)
	tmpFile := filepath.Join(tmpDir, "test_struct.go")
	err := os.WriteFile(tmpFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	// 2. Run Analyzer
	analyzer, err := NewGoAnalyzer(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create analyzer: %v", err)
	}

	err = analyzer.Analyze()
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// 3. Verify Results
	found := false
	for _, node := range analyzer.Nodes {
		if node.Name == "MyStruct" {
			found = true
			expectedSource := "// MyStruct is a test struct\ntype MyStruct struct {\n\tField1 string\n}"
			// Normalize newlines for cross-platform check (simplified)
			if strings.TrimSpace(node.SourceCode) != strings.TrimSpace(expectedSource) {
				t.Errorf("SourceCode mismatch.\nExpected:\n%s\nGot:\n%s", expectedSource, node.SourceCode)
			}
			if !node.HasDocstring {
				t.Error("Expected HasDocstring to be true")
			}
			if strings.TrimSpace(node.Docstring) != "MyStruct is a test struct" {
				t.Errorf("Docstring mismatch. Got: %s", node.Docstring)
			}
		}
	}
	if !found {
		t.Error("MyStruct node not found")
	}
}

func TestAnalyzeFunction(t *testing.T) {
	content := `package testpkg

func Submit(v int) {
	println(v)
}
`
	tmpDir := t.TempDir()
	writeGoMod(t, tmpDir)
	tmpFile := filepath.Join(tmpDir, "test_func.go")
	err := os.WriteFile(tmpFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	analyzer, err := NewGoAnalyzer(tmpDir)
	if err != nil {
		t.Fatalf("Failed to init analyzer: %v", err)
	}
	err = analyzer.Analyze()
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	found := false
	for _, node := range analyzer.Nodes {
		if node.Name == "Submit" {
			found = true
			if node.ComponentType != "function" {
				t.Errorf("Expected ComponentType function, got %s", node.ComponentType)
			}
			if len(node.Parameters) != 1 || node.Parameters[0] != "v" {
				t.Errorf("Expected params [v], got %v", node.Parameters)
			}
		}
	}
	if !found {
		t.Error("Submit function node not found")
	}
}

func TestAnalyzeCalls(t *testing.T) {
	content := `package testpkg

func Caller() {
	Callee()
}

func Callee() {}
`
	tmpDir := t.TempDir()
	writeGoMod(t, tmpDir)
	tmpFile := filepath.Join(tmpDir, "test_calls.go")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	analyzer, _ := NewGoAnalyzer(tmpDir)
	analyzer.Analyze()

	found := false
	for _, rel := range analyzer.Relationships {
		// Expect testpkg.Caller -> testpkg.Callee
		if strings.Contains(rel.Caller, "Caller") && strings.Contains(rel.Callee, "Callee") {
			found = true
			if rel.RelationshipType != "calls" {
				t.Errorf("Expected relationship type 'calls', got '%s'", rel.RelationshipType)
			}
		}
	}
	if !found {
		t.Error("Call relationship Caller->Callee not found")
	}
}

func TestAnalyzeMethodCalls(t *testing.T) {
	content := `package testpkg

type T struct{}

func (t *T) A() {
	t.B()
}

func (t *T) B() {}
`
	tmpDir := t.TempDir()
	writeGoMod(t, tmpDir)
	tmpFile := filepath.Join(tmpDir, "test_method_calls.go")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	analyzer, _ := NewGoAnalyzer(tmpDir)
	analyzer.Analyze()

	var resolved *bool
	var callee string
	for _, rel := range analyzer.Relationships {
		if strings.Contains(rel.Caller, "A") && strings.Contains(rel.Callee, "B") {
			r := rel.IsResolved
			resolved = &r
			callee = rel.Callee
		}
	}

	if resolved == nil {
		t.Error("Method call relationship A->B not found")
	} else if !*resolved {
		t.Errorf("Expected method call to be resolved (is_resolved=true), got callee %s", callee)
	}

	if callee != "" && !strings.Contains(callee, ".T.B") {
		t.Errorf("Expected callee to include type-qualified method ('.T.B'), got %s", callee)
	}
}

func TestAnalyzeCrossFileMethodCalls(t *testing.T) {
	contentA := `package testpkg

type A struct{}

func (a *A) Foo() {
	b := &B{}
	b.Bar()
}
`
	contentB := `package testpkg

type B struct{}

func (b *B) Bar() {}
`
	tmpDir := t.TempDir()
	writeGoMod(t, tmpDir)
	fileA := filepath.Join(tmpDir, "a.go")
	fileB := filepath.Join(tmpDir, "b.go")
	if err := os.WriteFile(fileA, []byte(contentA), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte(contentB), 0644); err != nil {
		t.Fatal(err)
	}

	analyzer, _ := NewGoAnalyzer(tmpDir)
	analyzer.Analyze()

	var resolved *bool
	var callee string
	for _, rel := range analyzer.Relationships {
		if strings.Contains(rel.Caller, "Foo") && strings.Contains(rel.Callee, "Bar") {
			r := rel.IsResolved
			resolved = &r
			callee = rel.Callee
		}
	}

	if resolved == nil {
		t.Error("Cross-file method call relationship Foo->Bar not found")
	} else if !*resolved {
		t.Errorf("Expected cross-file method call to be resolved (is_resolved=true), got callee %s", callee)
	}

	if callee != "" && !strings.Contains(callee, ".B.Bar") {
		t.Errorf("Expected callee to include type-qualified method ('.B.Bar'), got %s", callee)
	}
}

func TestIsResolved(t *testing.T) {
	content := `package testpkg

func Caller() {
	InternalFunc()  // same file - should be resolved
	fmt.Println()   // external package - should NOT be resolved
}

func InternalFunc() {}
`
	tmpDir := t.TempDir()
	writeGoMod(t, tmpDir)
	tmpFile := filepath.Join(tmpDir, "test_resolved.go")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	analyzer, _ := NewGoAnalyzer(tmpDir)
	analyzer.Analyze()

	var internalResolved, externalResolved *bool
	for _, rel := range analyzer.Relationships {
		if strings.Contains(rel.Callee, "InternalFunc") {
			r := rel.IsResolved
			internalResolved = &r
		}
		if strings.Contains(rel.Callee, "fmt.Println") {
			r := rel.IsResolved
			externalResolved = &r
		}
	}

	if internalResolved == nil {
		t.Error("InternalFunc call relationship not found")
	} else if !*internalResolved {
		t.Error("Expected InternalFunc call to be resolved (is_resolved=true)")
	}

	if externalResolved == nil {
		t.Error("fmt.Println call relationship not found")
	} else if *externalResolved {
		t.Error("Expected fmt.Println call to NOT be resolved (is_resolved=false)")
	}
}
