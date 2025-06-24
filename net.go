package net

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// CreateReplacedNetPkgOverlayFile create an Overlay file to replace net.Listen and net.Dialer.DialContext with functions from wasi-go-net.
func CreateReplacedNetPkgOverlayFile(ctx context.Context) (*OverlayFile, error) {
	srcs, err := GetReplacedNetSources(ctx)
	if err != nil {
		return nil, err
	}
	return CreateOverlayFile(srcs...)
}

type ReplacedNetSource struct {
	Path    string
	Content []byte
}

// GetReplacedNetSources return the source code after replacing net.Listen and net.Dialer.DialContext with functions from wasi-go-net.
func GetReplacedNetSources(ctx context.Context) ([]*ReplacedNetSource, error) {
	netPkgFiles, err := netPkgGoFiles(ctx)
	if err != nil {
		return nil, err
	}
	paths := findSourcePaths(
		netPkgFiles,
		func(decl *ast.FuncDecl) bool {
			if decl.Name.Name != "DialContext" {
				return false
			}
			if decl.Recv == nil {
				return false
			}
			if len(decl.Recv.List) == 0 {
				return false
			}
			if len(decl.Recv.List[0].Names) == 0 {
				return false
			}
			star, ok := decl.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				return false
			}
			ident, ok := star.X.(*ast.Ident)
			if !ok {
				return false
			}
			return ident.Name == "Dialer"
		},
		func(decl *ast.FuncDecl) bool {
			return decl.Name.Name == "Listen" && decl.Recv == nil
		},
	)
	if len(paths) == 0 {
		return nil, errors.New("failed to find net package source files")
	}

	ret := make([]*ReplacedNetSource, 0, len(paths))
	for _, path := range paths {
		content, err := createReplacedNetSource(path)
		if err != nil {
			return nil, err
		}
		ret = append(ret, &ReplacedNetSource{
			Path:    path,
			Content: content,
		})
	}
	return ret, nil
}

type OverlayFile struct {
	path         string
	tmpFilePaths []string
}

func (f *OverlayFile) Path() string {
	return f.path
}

func (f *OverlayFile) Close() {
	_ = os.Remove(f.path)
	for _, path := range f.tmpFilePaths {
		_ = os.Remove(path)
	}
}

// CreateOverlayFile create an overlay file from the source code where net.Listen and net.Dialer.DialContext have been replaced.
func CreateOverlayFile(srcs ...*ReplacedNetSource) (*OverlayFile, error) {
	tmpFilePaths := make([]string, 0, len(srcs))
	overlayMap := make(map[string]string)
	for _, src := range srcs {
		tmpFile, err := os.CreateTemp("", filepath.Base(src.Path)+"_")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp file: %w", err)
		}
		defer tmpFile.Close()

		if _, err := tmpFile.Write(src.Content); err != nil {
			return nil, fmt.Errorf("failed to write file content: %w", err)
		}
		tmpFilePaths = append(tmpFilePaths, tmpFile.Name())
		overlayMap[src.Path] = tmpFile.Name()
	}
	tmpFile, err := os.CreateTemp("", "wasi_go_net_overlay")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	content, err := json.Marshal(map[string]interface{}{
		"Replace": overlayMap,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create overlay file content: %w", err)
	}
	if _, err := tmpFile.Write(content); err != nil {
		return nil, fmt.Errorf("failed to write file content: %w", err)
	}
	return &OverlayFile{
		path:         tmpFile.Name(),
		tmpFilePaths: tmpFilePaths,
	}, nil
}

func netPkgGoFiles(ctx context.Context) ([]string, error) {
	dir, err := netPkgDir(ctx)
	if err != nil {
		return nil, err
	}
	var ret []string
	_ = filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(info.Name()) != ".go" {
			return nil
		}

		if strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}

		ret = append(ret, path)
		return nil
	})
	return ret, nil
}

func netPkgDir(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "go", "env", "GOROOT").CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("failed to get GOROOT: %w", err)
	}
	goroot := strings.TrimSpace(string(out))
	return filepath.Join(goroot, "src", "net"), nil
}

func findSourcePaths(netPkgFiles []string, matchers ...func(*ast.FuncDecl) bool) []string {
	sourcePathMap := make(map[string]struct{})
	fset := token.NewFileSet()
	for _, netPkgFile := range netPkgFiles {
		src, err := os.ReadFile(netPkgFile)
		if err != nil {
			continue
		}

		file, err := parser.ParseFile(fset, netPkgFile, src, 0)
		if err != nil {
			continue
		}

		for _, decl := range file.Decls {
			funcDecl, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			for _, matcher := range matchers {
				if matcher(funcDecl) {
					sourcePathMap[netPkgFile] = struct{}{}
				}
			}
		}
	}
	paths := make([]string, 0, len(sourcePathMap))
	for path := range sourcePathMap {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// createModifiedNetFile creates a override version of a net package file using AST manipulation.
func createReplacedNetSource(path string) ([]byte, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, filepath.Base(path), src, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s: %w", path, err)
	}

	// Add unsafe import if not present
	hasUnsafeImport := false
	for _, imp := range astFile.Imports {
		if imp.Path.Value == `"unsafe"` {
			hasUnsafeImport = true
			break
		}
	}

	if !hasUnsafeImport {
		unsafeImport := &ast.ImportSpec{
			Name: &ast.Ident{Name: "_"},
			Path: &ast.BasicLit{Kind: token.STRING, Value: `"unsafe"`},
		}
		astFile.Imports = append(astFile.Imports, unsafeImport)

		// Find the last import in the file to determine where to insert
		var lastImportDecl *ast.GenDecl
		for _, decl := range astFile.Decls {
			if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
				lastImportDecl = genDecl
			}
		}

		if lastImportDecl != nil {
			lastImportDecl.Specs = append(lastImportDecl.Specs, unsafeImport)
		} else {
			// Create new import declaration
			importDecl := &ast.GenDecl{
				Tok:   token.IMPORT,
				Specs: []ast.Spec{unsafeImport},
			}
			astFile.Decls = append([]ast.Decl{importDecl}, astFile.Decls...)
		}
	}

	var (
		foundDialContext bool
		foundListen      bool
	)

	// Find the target functions and modify them
	for _, decl := range astFile.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			funcName := funcDecl.Name.Name
			switch {
			case funcName == "DialContext" && funcDecl.Recv != nil:
				// replace function body of DialContext method.
				//
				// func (d *Dialer) DialContext(ctx context.Context, network, address string) (Conn, error) {
				//   return _dialContext(ctx, network, address)
				// }
				funcDecl.Body = &ast.BlockStmt{
					List: []ast.Stmt{
						&ast.ReturnStmt{
							Results: []ast.Expr{
								&ast.CallExpr{
									Fun: &ast.Ident{Name: "_dialContext"},
									Args: []ast.Expr{
										&ast.Ident{Name: "ctx"},
										&ast.Ident{Name: "network"},
										&ast.Ident{Name: "address"},
									},
								},
							},
						},
					},
				}
				foundDialContext = true
			case funcName == "Listen" && funcDecl.Recv == nil:
				// replace function body of Listen function.
				//
				// func Listen(network, address string) (Listener, error) {
				//   return _listen(network, address)
				// }
				funcDecl.Body = &ast.BlockStmt{
					List: []ast.Stmt{
						&ast.ReturnStmt{
							Results: []ast.Expr{
								&ast.CallExpr{
									Fun: &ast.Ident{Name: "_listen"},
									Args: []ast.Expr{
										&ast.Ident{Name: "network"},
										&ast.Ident{Name: "address"},
									},
								},
							},
						},
					},
				}
				foundListen = true
			}
		}
	}

	// Check if at least one target function was found
	if !foundDialContext && !foundListen {
		return nil, fmt.Errorf("no target functions (DialContext or Listen) found in %s", path)
	}

	// //go:linkname _dialContext github.com/goccy/wasi-go-net/wasip1.DialContext
	// func _dialContext(ctx context.Context, network, address string) (Conn, error)
	dialContextFuncDecl := &ast.FuncDecl{
		Doc: &ast.CommentGroup{
			List: []*ast.Comment{
				{Text: "//go:linkname _dialContext github.com/goccy/wasi-go-net/wasip1.DialContext"},
			},
		},
		Name: &ast.Ident{Name: "_dialContext"},
		Type: &ast.FuncType{
			Params: &ast.FieldList{
				List: []*ast.Field{
					{
						Names: []*ast.Ident{{Name: "ctx"}},
						Type:  &ast.SelectorExpr{X: &ast.Ident{Name: "context"}, Sel: &ast.Ident{Name: "Context"}},
					},
					{
						Names: []*ast.Ident{{Name: "network"}},
						Type:  &ast.Ident{Name: "string"},
					},
					{
						Names: []*ast.Ident{{Name: "address"}},
						Type:  &ast.Ident{Name: "string"},
					},
				},
			},
			Results: &ast.FieldList{
				List: []*ast.Field{
					{Type: &ast.Ident{Name: "Conn"}},
					{Type: &ast.Ident{Name: "error"}},
				},
			},
		},
		Body: nil, // No body for external linkage
	}

	// //go:linkname _listen github.com/goccy/wasi-go-net/wasip1.Listen
	// func _listen(network, address string) (Listener, error)
	listenFuncDecl := &ast.FuncDecl{
		Doc: &ast.CommentGroup{
			List: []*ast.Comment{
				{Text: "//go:linkname _listen github.com/goccy/wasi-go-net/wasip1.Listen"},
			},
		},
		Name: &ast.Ident{Name: "_listen"},
		Type: &ast.FuncType{
			Params: &ast.FieldList{
				List: []*ast.Field{
					{
						Names: []*ast.Ident{{Name: "network"}},
						Type:  &ast.Ident{Name: "string"},
					},
					{
						Names: []*ast.Ident{{Name: "address"}},
						Type:  &ast.Ident{Name: "string"},
					},
				},
			},
			Results: &ast.FieldList{
				List: []*ast.Field{
					{Type: &ast.Ident{Name: "Listener"}},
					{Type: &ast.Ident{Name: "error"}},
				},
			},
		},
		Body: nil, // No body for external linkage
	}

	if foundDialContext {
		var lastPos token.Pos
		if len(astFile.Decls) != 0 {
			lastPos = astFile.Decls[len(astFile.Decls)-1].End()
		}

		dialContextFuncDecl.Type.Func = lastPos + 1
		astFile.Decls = append(astFile.Decls, dialContextFuncDecl)
	}
	if foundListen {
		var lastPos token.Pos
		if len(astFile.Decls) != 0 {
			lastPos = astFile.Decls[len(astFile.Decls)-1].End()
		}

		listenFuncDecl.Type.Func = lastPos + 1
		astFile.Decls = append(astFile.Decls, listenFuncDecl)
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, astFile); err != nil {
		return nil, fmt.Errorf("failed to format without comment AST: %w", err)
	}

	return buf.Bytes(), nil
}
