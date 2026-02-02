// Package analyzer provides functionality to analyze Go source code
// and extract structural information such as AST nodes and call relationships.
package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/don7panic/codewiki-go-analyzer/models"
)

type GoAnalyzer struct {
	RepoPath         string
	RepoAbs          string
	FileSet          *token.FileSet
	Nodes            []models.Node
	Relationships    []models.CallRelationship
	CollectedNodeIDs map[string]bool // Track collected node IDs for is_resolved
}

func NewGoAnalyzer(repoPath string) (*GoAnalyzer, error) {
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, err
	}

	return &GoAnalyzer{
		RepoPath:         repoPath,
		RepoAbs:          repoAbs,
		FileSet:          token.NewFileSet(),
		Nodes:            []models.Node{},
		Relationships:    []models.CallRelationship{},
		CollectedNodeIDs: make(map[string]bool),
	}, nil
}

func (a *GoAnalyzer) Analyze() error {
	moduleRoots, err := a.findModuleRoots()
	if err != nil {
		return err
	}
	if len(moduleRoots) == 0 {
		moduleRoots = []string{a.RepoAbs}
	}

	fileInfos := map[string]*fileInfo{}

	for _, root := range moduleRoots {
		pkgs, loadErr := a.loadPackages(root)
		if loadErr != nil {
			return loadErr
		}

		for _, pkg := range pkgs {
			for _, file := range pkg.Syntax {
				filename := a.FileSet.Position(file.Pos()).Filename
				if filename == "" || isTestFile(filename) {
					continue
				}
				absPath, absErr := filepath.Abs(filename)
				if absErr == nil {
					filename = absPath
				}
				if !isPathInRepo(a.RepoAbs, filename) {
					continue
				}
				if _, exists := fileInfos[filename]; exists {
					continue
				}
				content, readErr := os.ReadFile(filename)
				if readErr != nil {
					return readErr
				}
				fileInfos[filename] = &fileInfo{
					file:    file,
					info:    pkg.TypesInfo,
					pkg:     pkg.Types,
					content: content,
				}
			}
		}
	}

	// First pass: Collect nodes (Structs, Interfaces, Functions, Methods)
	for filename, info := range fileInfos {
		a.collectNodes(filename, info)
	}

	// Second pass: Collect relationships (Calls)
	for filename, info := range fileInfos {
		a.collectCalls(filename, info)
	}

	return nil
}

func (a *GoAnalyzer) loadPackages(root string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode:  packages.NeedName | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedFiles,
		Dir:   root,
		Fset:  a.FileSet,
		Tests: false,
	}
	return packages.Load(cfg, "./...")
}

func (a *GoAnalyzer) findModuleRoots() ([]string, error) {
	if _, err := os.Stat(filepath.Join(a.RepoAbs, "go.work")); err == nil {
		return []string{a.RepoAbs}, nil
	}

	roots := []string{}
	err := filepath.WalkDir(a.RepoAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			roots = append(roots, filepath.Dir(path))
		}
		return nil
	})
	return roots, err
}

func isTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go")
}

type fileInfo struct {
	file    *ast.File
	info    *types.Info
	pkg     *types.Package
	content []byte
}

func (a *GoAnalyzer) getComponentIDForFile(filePath string, name string, receiverType string) string {
	// Mimic CodeWiki's ID generation: module_path.name
	// models/Node.ID usually is fully qualified.

	// We replace path.Dir separators to dots
	relPath, _ := filepath.Rel(a.RepoAbs, filePath)
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

func (a *GoAnalyzer) getComponentIDForPos(pos token.Pos, name string, receiverType string) string {
	if pos == token.NoPos || a.FileSet == nil {
		return ""
	}
	filename := a.FileSet.Position(pos).Filename
	if filename == "" {
		return ""
	}
	absPath, err := filepath.Abs(filename)
	if err == nil {
		filename = absPath
	}
	return a.getComponentIDForFile(filename, name, receiverType)
}

func (a *GoAnalyzer) isPosInRepo(pos token.Pos) bool {
	if pos == token.NoPos || a.FileSet == nil {
		return false
	}
	filename := a.FileSet.Position(pos).Filename
	if filename == "" {
		return false
	}
	absPath, err := filepath.Abs(filename)
	if err == nil {
		filename = absPath
	}
	return isPathInRepo(a.RepoAbs, filename)
}

func isPathInRepo(repoAbs string, path string) bool {
	repoAbs = filepath.Clean(repoAbs)
	path = filepath.Clean(path)
	if repoAbs == path {
		return true
	}
	return strings.HasPrefix(path, repoAbs+string(os.PathSeparator))
}

func (a *GoAnalyzer) collectNodes(filePath string, info *fileInfo) {
	ast.Inspect(info.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GenDecl:
			if x.Tok == token.TYPE {
				for _, spec := range x.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						a.visitTypeSpec(ts, x.Doc, filePath, info.content)
					}
				}
			}
		case *ast.FuncDecl:
			a.visitFuncDecl(x, filePath, info.content)
		}
		return true
	})
}

func (a *GoAnalyzer) collectCalls(filePath string, info *fileInfo) {
	ast.Inspect(info.file, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok {
			a.visitFuncBodyForCalls(fn, filePath, info.info, info.pkg)
		}
		return true
	})
}

func (a *GoAnalyzer) visitTypeSpec(ts *ast.TypeSpec, genDeclDoc *ast.CommentGroup, filePath string, content []byte) {
	nodeType := "struct"
	if _, ok := ts.Type.(*ast.InterfaceType); ok {
		nodeType = "interface"
	} else if _, ok := ts.Type.(*ast.StructType); ok {
		nodeType = "struct"
	} else {
		return // Skip other types for now
	}

	relativePath, _ := filepath.Rel(a.RepoAbs, filePath)
	componentID := a.getComponentIDForFile(filePath, ts.Name.Name, "")

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
	if startOffset >= 0 && endOffset <= len(content) && startOffset <= endOffset {
		sourceCode = string(content[startOffset:endOffset])
	}

	node := models.Node{
		ID:            componentID,
		Name:          ts.Name.Name,
		ComponentType: "class", // Mapping struct/interface to "class" for CodeWiki compatibility
		FilePath:      filePath,
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

func (a *GoAnalyzer) visitFuncDecl(fn *ast.FuncDecl, filePath string, content []byte) {
	relativePath, _ := filepath.Rel(a.RepoAbs, filePath)
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
		componentID = a.getComponentIDForFile(filePath, fn.Name.Name, recvType)
		displayName = fmt.Sprintf("method %s.%s", recvType, fn.Name.Name)
	} else {
		// Regular function
		componentID = a.getComponentIDForFile(filePath, fn.Name.Name, "")
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
	if startOffset >= 0 && endOffset <= len(content) && startOffset <= endOffset {
		sourceCode = string(content[startOffset:endOffset])
	}

	node := models.Node{
		ID:            componentID,
		Name:          fn.Name.Name,
		ComponentType: componentType,
		FilePath:      filePath,
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

func (a *GoAnalyzer) visitFuncBodyForCalls(fn *ast.FuncDecl, filePath string, typeInfo *types.Info, typePkg *types.Package) {
	if fn.Body == nil {
		return
	}

	callerID := ""
	recvName := ""
	recvType := ""
	if fn.Recv != nil {
		for _, field := range fn.Recv.List {
			typeStr := typeToString(field.Type)
			if len(typeStr) > 0 && typeStr[0] == '*' {
				recvType = typeStr[1:]
			} else {
				recvType = typeStr
			}
			if len(field.Names) > 0 {
				recvName = field.Names[0].Name
			}
		}
		callerID = a.getComponentIDForFile(filePath, fn.Name.Name, recvType)
	} else {
		callerID = a.getComponentIDForFile(filePath, fn.Name.Name, "")
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			a.processCall(callerID, recvName, recvType, call, typeInfo, typePkg, filePath)
		}
		return true
	})
}

func (a *GoAnalyzer) processCall(callerID string, recvName string, recvType string, call *ast.CallExpr, typeInfo *types.Info, typePkg *types.Package, filePath string) {
	if typeInfo != nil && typePkg != nil {
		if calleeName, resolved, ok := a.resolveCallWithTypes(call, typeInfo, typePkg); ok {
			if calleeName != "" {
				rel := models.CallRelationship{
					Caller:           callerID,
					Callee:           calleeName,
					CallLine:         a.FileSet.Position(call.Pos()).Line,
					RelationshipType: "calls",
					IsResolved:       resolved,
				}
				a.Relationships = append(a.Relationships, rel)
			}
			return
		}
	}

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
			calleeName = a.getComponentIDForFile(filePath, calleeName, "")
		}

	case *ast.SelectorExpr:
		// Method or package call: pkg.Func() or obj.Method()
		if xIdent, ok := fun.X.(*ast.Ident); ok {
			// If this is a call on the current method receiver, resolve to method ID.
			if recvName != "" && recvType != "" && xIdent.Name == recvName {
				calleeName = a.getComponentIDForFile(filePath, fun.Sel.Name, recvType)
			} else {
				calleeName = fmt.Sprintf("%s.%s", xIdent.Name, fun.Sel.Name)
			}
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

func (a *GoAnalyzer) resolveCallWithTypes(call *ast.CallExpr, typeInfo *types.Info, typePkg *types.Package) (string, bool, bool) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		obj := typeInfo.Uses[fun]
		switch fn := obj.(type) {
		case *types.Func:
			calleeName := a.getComponentIDForPos(fn.Pos(), fn.Name(), "")
			if calleeName != "" && a.isPosInRepo(fn.Pos()) {
				return calleeName, a.CollectedNodeIDs[calleeName], true
			}
			if fn.Pkg() != nil {
				return fmt.Sprintf("%s.%s", fn.Pkg().Name(), fn.Name()), false, true
			}
			return fn.Name(), false, true
		case *types.Builtin:
			return fun.Name, false, true
		default:
			return "", false, false
		}

	case *ast.SelectorExpr:
		if sel := typeInfo.Selections[fun]; sel != nil {
			if fn, ok := sel.Obj().(*types.Func); ok {
				recvType := receiverTypeString(fn.Type())
				calleeName := a.getComponentIDForPos(fn.Pos(), fn.Name(), recvType)
				if calleeName != "" && a.isPosInRepo(fn.Pos()) {
					return calleeName, a.CollectedNodeIDs[calleeName], true
				}
				// External method call on a value; fall back to a type-qualified name.
				recvStr := types.TypeString(sel.Recv(), func(pkg *types.Package) string {
					if pkg == typePkg {
						return ""
					}
					return pkg.Name()
				})
				return fmt.Sprintf("%s.%s", recvStr, fn.Name()), false, true
			}
			return "", false, false
		}

		if xIdent, ok := fun.X.(*ast.Ident); ok {
			if _, ok := typeInfo.Uses[xIdent].(*types.PkgName); ok {
				if obj := typeInfo.Uses[fun.Sel]; obj != nil {
					if fn, ok := obj.(*types.Func); ok {
						calleeName := a.getComponentIDForPos(fn.Pos(), fn.Name(), "")
						if calleeName != "" && a.isPosInRepo(fn.Pos()) {
							return calleeName, a.CollectedNodeIDs[calleeName], true
						}
						return fmt.Sprintf("%s.%s", xIdent.Name, fn.Name()), false, true
					}
				}
			}
		}
	}

	return "", false, false
}

func receiverTypeString(t types.Type) string {
	sig, ok := t.(*types.Signature)
	if !ok {
		return ""
	}
	recv := sig.Recv()
	if recv == nil {
		return ""
	}
	recvType := recv.Type()
	if ptr, ok := recvType.(*types.Pointer); ok {
		recvType = ptr.Elem()
	}
	return types.TypeString(recvType, func(pkg *types.Package) string { return "" })
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
