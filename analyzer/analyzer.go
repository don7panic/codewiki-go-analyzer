// Package analyzer provides functionality to analyze Go source code
// and extract structural information such as AST nodes and call relationships.
package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"

	"github.com/don7panic/codewiki-go-analyzer/models"
)

type GoAnalyzer struct {
	FilePath         string
	Content          []byte
	RepoPath         string
	FileSet          *token.FileSet
	Nodes            []models.Node
	Relationships    []models.CallRelationship
	PackageName      string
	CollectedNodeIDs map[string]bool // Track collected node IDs for is_resolved
}

func NewGoAnalyzer(filePath string, repoPath string) (*GoAnalyzer, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	return &GoAnalyzer{
		FilePath:         filePath,
		Content:          content,
		RepoPath:         repoPath,
		FileSet:          token.NewFileSet(),
		Nodes:            []models.Node{},
		Relationships:    []models.CallRelationship{},
		CollectedNodeIDs: make(map[string]bool),
	}, nil
}

func (a *GoAnalyzer) Analyze() error {
	f, err := parser.ParseFile(a.FileSet, a.FilePath, a.Content, parser.ParseComments)
	if err != nil {
		return err
	}

	a.PackageName = f.Name.Name

	// First pass: Collect nodes (Structs, Interfaces, Functions, Methods)
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GenDecl:
			if x.Tok == token.TYPE {
				for _, spec := range x.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						a.visitTypeSpec(ts, x.Doc)
					}
				}
			}
		case *ast.FuncDecl:
			a.visitFuncDecl(x)
		}
		return true
	})

	// Second pass: Collect relationships (Calls)
	ast.Inspect(f, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok {
			a.visitFuncBodyForCalls(fn)
		}
		return true
	})

	return nil
}

// Generate a unique module-like path from the file path relative to repo root
func (a *GoAnalyzer) getModulePath() string {
	relPath, err := filepath.Rel(a.RepoPath, a.FilePath)
	if err != nil {
		relPath = a.FilePath
	}
	dir := filepath.Dir(relPath)
	if dir == "." {
		return a.PackageName
	}
	// Convert path separators to dots for ID consistency
	// e.g. "src/utils" -> "src.utils"
	// Then append package name so "src/utils" (package "utils") -> "src.utils.utils"
	// Wait, usually Go module path ends with package name.
	// Let's just use slash-to-dot replacement of directory + package name.
	// But to match Tree-sitter behavior in CodeWiki:
	// It strips extension and uses the file path.
	// Let's stick to Python implementation: rel_path.replace('/', '.')
	// But removing extension and potentially filename?
	// The Python impl uses: module_path = file_path_without_ext.replace('/', '.')
	// Let's do that for maximum compatibility.

	ext := filepath.Ext(relPath)
	pathNoExt := relPath[:len(relPath)-len(ext)]
	return filepath.ToSlash(pathNoExt) // Replace \ with / first on Windows? Text replace is safer.
}

func (a *GoAnalyzer) getComponentID(name string, receiverType string) string {
	// Mimic CodeWiki's ID generation: module_path.name
	// models/Node.ID usually is fully qualified.

	// We replace path.Dir separators to dots
	relPath, _ := filepath.Rel(a.RepoPath, a.FilePath)
	ext := filepath.Ext(relPath)
	pathNoExt := relPath[:len(relPath)-len(ext)]
	modulePath := ""

	// Simple replace all separators with dots
	// Note: This relies on standard forward slashes or OS separators
	for _, c := range pathNoExt {
		if os.IsPathSeparator(uint8(c)) {
			modulePath += "."
		} else {
			modulePath += string(c)
		}
	}

	if receiverType != "" {
		return fmt.Sprintf("%s.%s.%s", modulePath, receiverType, name)
	}
	return fmt.Sprintf("%s.%s", modulePath, name)
}

func (a *GoAnalyzer) visitTypeSpec(ts *ast.TypeSpec, genDeclDoc *ast.CommentGroup) {
	nodeType := "struct"
	if _, ok := ts.Type.(*ast.InterfaceType); ok {
		nodeType = "interface"
	} else if _, ok := ts.Type.(*ast.StructType); ok {
		nodeType = "struct"
	} else {
		return // Skip other types for now
	}

	relativePath, _ := filepath.Rel(a.RepoPath, a.FilePath)
	componentID := a.getComponentID(ts.Name.Name, "")

	startPos := a.FileSet.Position(ts.Pos())
	endPos := a.FileSet.Position(ts.End())

	// Effective Doc
	doc := ts.Doc
	if doc == nil {
		doc = genDeclDoc
	}

	// Capture source code
	var startOffset int
	if doc != nil {
		startOffset = a.FileSet.Position(doc.Pos()).Offset
	} else {
		startOffset = startPos.Offset
	}
	endOffset := endPos.Offset

	var sourceCode string
	if startOffset >= 0 && endOffset <= len(a.Content) && startOffset <= endOffset {
		sourceCode = string(a.Content[startOffset:endOffset])
	}

	node := models.Node{
		ID:            componentID,
		Name:          ts.Name.Name,
		ComponentType: "class", // Mapping struct/interface to "class" for CodeWiki compatibility
		FilePath:      a.FilePath,
		RelativePath:  relativePath,
		StartLine:     startPos.Line,
		EndLine:       endPos.Line,
		NodeType:      nodeType,
		ComponentID:   componentID,
		DisplayName:   fmt.Sprintf("%s %s", nodeType, ts.Name.Name),
		DependsOn:     []string{},
		SourceCode:    sourceCode,
	}

	if doc != nil {
		node.HasDocstring = true
		node.Docstring = doc.Text()
	}

	a.CollectedNodeIDs[componentID] = true
	a.Nodes = append(a.Nodes, node)
}

func typeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeToString(t.X)
	case *ast.SelectorExpr:
		return typeToString(t.X) + "." + t.Sel.Name
	case *ast.IndexExpr: // Generic[T]
		return typeToString(t.X) + "[" + typeToString(t.Index) + "]"
	case *ast.IndexListExpr: // Generic[T, U]
		// This is for multi-type generics
		// Simple approximation
		indices := ""
		for i, idx := range t.Indices {
			if i > 0 {
				indices += ", "
			}
			indices += typeToString(idx)
		}
		return typeToString(t.X) + "[" + indices + "]"
	default:
		return ""
	}
}

func (a *GoAnalyzer) visitFuncDecl(fn *ast.FuncDecl) {
	relativePath, _ := filepath.Rel(a.RepoPath, a.FilePath)
	startPos := a.FileSet.Position(fn.Pos())
	endPos := a.FileSet.Position(fn.End())

	var componentID string
	var className string
	var displayName string

	if fn.Recv != nil {
		// It's a method
		recvType := ""
		for _, field := range fn.Recv.List {
			// FIXED: Use improved typeToString to handle *pkg.Type, Generic[T], etc.
			typeStr := typeToString(field.Type)
			// Strip pointer for class name grouping if it's a pointer receiver
			if len(typeStr) > 0 && typeStr[0] == '*' {
				recvType = typeStr[1:]
			} else {
				recvType = typeStr
			}
		}
		className = recvType
		componentID = a.getComponentID(fn.Name.Name, recvType)
		displayName = fmt.Sprintf("method %s.%s", recvType, fn.Name.Name)
	} else {
		// Regular function
		componentID = a.getComponentID(fn.Name.Name, "")
		displayName = fmt.Sprintf("func %s", fn.Name.Name)
	}

	// Determine component type and display strings
	componentType := "function"
	nodeType := "function"
	if fn.Recv != nil {
		componentType = "method"
		nodeType = "method"
	}

	// Capture source code
	startOffset := startPos.Offset
	if fn.Doc != nil {
		startOffset = a.FileSet.Position(fn.Doc.Pos()).Offset
	}
	endOffset := endPos.Offset

	var sourceCode string
	if startOffset >= 0 && endOffset <= len(a.Content) && startOffset <= endOffset {
		sourceCode = string(a.Content[startOffset:endOffset])
	}

	node := models.Node{
		ID:            componentID,
		Name:          fn.Name.Name,
		ComponentType: componentType,
		FilePath:      a.FilePath,
		RelativePath:  relativePath,
		StartLine:     startPos.Line,
		EndLine:       endPos.Line,
		NodeType:      nodeType,
		ComponentID:   componentID,
		ClassName:     className,
		DisplayName:   displayName,
		DependsOn:     []string{},
		SourceCode:    sourceCode,
	}

	if fn.Doc != nil {
		node.HasDocstring = true
		node.Docstring = fn.Doc.Text()
	}

	// Extract parameters
	params := []string{}
	if fn.Type.Params != nil {
		for _, p := range fn.Type.Params.List {
			for _, name := range p.Names {
				params = append(params, name.Name)
			}
		}
	}
	node.Parameters = params

	a.CollectedNodeIDs[componentID] = true
	a.Nodes = append(a.Nodes, node)
}

func (a *GoAnalyzer) visitFuncBodyForCalls(fn *ast.FuncDecl) {
	if fn.Body == nil {
		return
	}

	callerID := ""
	if fn.Recv != nil {
		recvType := ""
		for _, field := range fn.Recv.List {
			typeStr := typeToString(field.Type)
			if len(typeStr) > 0 && typeStr[0] == '*' {
				recvType = typeStr[1:]
			} else {
				recvType = typeStr
			}
		}
		callerID = a.getComponentID(fn.Name.Name, recvType)
	} else {
		callerID = a.getComponentID(fn.Name.Name, "")
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			a.processCall(callerID, call)
		}
		return true
	})
}

func (a *GoAnalyzer) processCall(callerID string, call *ast.CallExpr) {
	var calleeName string

	switch fun := call.Fun.(type) {
	case *ast.Ident:
		// Simple call: funcName()
		// We capture just the name. The ID ambiguity remains for now (could be local or builtin),
		// but naming consistency is key.
		calleeName = fun.Name

		// Use full qualification guessing only for local module calls?
		// Actually, without types, best effort is to assume same-package call if not builtin.
		if !isBuiltin(calleeName) {
			// We TRY to use the same ID scheme: module_path.funcName
			// But we don't know if it's in a different file of same package.
			// Assumption: Same package/module.
			// For now, assume it's in the same module path context.
			// This is "best effort" as noted.
			calleeName = a.getComponentID(calleeName, "")
		}

	case *ast.SelectorExpr:
		// Method or package call: pkg.Func() or obj.Method()
		if xIdent, ok := fun.X.(*ast.Ident); ok {
			calleeName = fmt.Sprintf("%s.%s", xIdent.Name, fun.Sel.Name)
		}
	}

	if calleeName != "" {
		rel := models.CallRelationship{
			Caller:           callerID,
			Callee:           calleeName,
			CallLine:         a.FileSet.Position(call.Pos()).Line,
			RelationshipType: "calls",
			IsResolved:       a.CollectedNodeIDs[calleeName],
		}
		a.Relationships = append(a.Relationships, rel)
	}
}

func isBuiltin(name string) bool {
	// Go builtin functions
	builtinFuncs := map[string]bool{
		"append": true, "cap": true, "clear": true, "close": true, "complex": true,
		"copy": true, "delete": true, "imag": true, "len": true, "max": true, "min": true,
		"make": true, "new": true, "panic": true, "print": true, "println": true,
		"real": true, "recover": true,
	}

	// Go builtin types
	builtinTypes := map[string]bool{
		"bool": true, "byte": true, "complex64": true, "complex128": true,
		"error": true, "float32": true, "float64": true,
		"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
		"rune": true, "string": true,
		"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true,
		"any": true, "comparable": true,
	}

	// Go builtin constants
	builtinConsts := map[string]bool{
		"true": true, "false": true, "nil": true, "iota": true,
	}

	return builtinFuncs[name] || builtinTypes[name] || builtinConsts[name]
}
