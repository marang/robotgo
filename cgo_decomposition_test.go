package robotgo

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Source constraints complement the frozen declaration gate: policy must not
// create a second native translation unit or silently narrow native key codes.
func TestCGODecompositionPolicyAndOwners(t *testing.T) {
	files := []string{
		"robotgo.go", "key.go", "bitmap_facade_cgo.go", "capabilities_cgo.go",
		"capture_facade_cgo.go", "capture_policy_cgo.go", "capture_state_cgo.go",
		"clipboard_helpers_cgo.go", "display_policy_cgo.go", "errors_cgo.go",
		"input_lifecycle_cgo.go", "key_constants_cgo.go", "key_helpers_cgo.go",
		"key_ownership_cgo.go", "key_policy_cgo.go", "key_text_cgo.go",
		"mouse_convenience_cgo.go", "mouse_ownership_cgo.go", "mouse_policy_cgo.go",
		"runtime_types_cgo.go", "utilities_cgo.go", "wayland_bounds_cache_cgo.go",
		"wayland_bounds_parser_cgo.go", "window_facade_cgo.go",
	}
	for _, name := range append(append([]string{}, files...), "native_adapter_cgo.go", "key_native_cgo.go") {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if lines := bytes.Count(source, []byte{'\n'}); lines >= 1000 {
			t.Errorf("%s has %d lines; handwritten split files must stay below 1000", name, lines)
		}
		if name == "native_adapter_cgo.go" || name == "key_native_cgo.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); path == "C" {
				t.Errorf("%s imports C instead of crossing its private Go seam", name)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if selector, ok := node.(*ast.SelectorExpr); ok {
				if ident, ok := selector.X.(*ast.Ident); ok && ident.Name == "C" {
					t.Errorf("%s contains direct C access", name)
				}
			}
			return true
		})
	}
	owners := map[string]string{
		"mouse/mouse_c.h":                "native_adapter_cgo.go",
		"window/goWindow.h":              "native_adapter_cgo.go",
		"window/goWindow_wayland_stub.h": "native_adapter_cgo.go",
		"key/keypress_c.h":               "key_native_cgo.go",
	}
	rootFiles, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for header, owner := range owners {
		var found []string
		for _, name := range rootFiles {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			source, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			for range bytes.Count(source, []byte("#include \""+header+"\"")) {
				found = append(found, name)
			}
		}
		if len(found) != 1 || found[0] != owner {
			t.Errorf("implementation header %s owned by %v, want only %s", header, found, owner)
		}
	}
}

func TestCGONativeKeyCodeWidthStaysNative(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "key_native_cgo.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	isNativeCode := func(expr ast.Expr) bool {
		selector, ok := expr.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "MMKeyCode" {
			return false
		}
		ident, ok := selector.X.(*ast.Ident)
		return ok && ident.Name == "C"
	}
	var mapping, checked, tap, toggle bool
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.ValueSpec:
			if len(node.Names) == 1 && node.Names[0].Name == "keyNames" && len(node.Values) == 1 {
				literal, ok := node.Values[0].(*ast.CompositeLit)
				if ok {
					mapType, ok := literal.Type.(*ast.MapType)
					mapping = ok && isNativeCode(mapType.Value)
				}
			}
		case *ast.FuncDecl:
			switch node.Name.Name {
			case "checkKeyCodes":
				checked = node.Type.Results != nil && isNativeCode(node.Type.Results.List[0].Type)
			case "tapKeyCode":
				tap = isNativeCode(node.Type.Params.List[0].Type)
			case "nativeToggleKey":
				// The inferred result of checkKeyCodes must reach C.toggleKeyCode
				// unchanged; no intermediate ordinary-Go key-code conversion.
				ast.Inspect(node.Body, func(child ast.Node) bool {
					call, ok := child.(*ast.CallExpr)
					if !ok {
						return true
					}
					selector, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "toggleKeyCode" || len(call.Args) == 0 {
						return true
					}
					owner, ok := selector.X.(*ast.Ident)
					if !ok || owner.Name != "C" {
						return true
					}
					key, ok := call.Args[0].(*ast.Ident)
					toggle = ok && key.Name == "key"
					return true
				})
			}
		}
		return true
	})
	if !mapping || !checked || !tap || !toggle {
		t.Fatalf("native key code type lost: mapping=%t checked=%t tap=%t toggle=%t", mapping, checked, tap, toggle)
	}
}
