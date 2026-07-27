package apicompat

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"
)

func TestRenderPackageAPI(t *testing.T) {
	t.Parallel()

	pkg := checkSource(t, `
package fixture

const Answer = 42
var Current Public

type Public struct {
	Visible int `+"`json:\"visible\"`"+`
	hidden string
}
type Alias = Public
type Contract interface {
	Do(string) error
}

func Function(Public, ...string) (Alias, error) { panic("fixture") }
func (Public) Value(int) string { return "" }
func (*Public) Pointer() {}
`)
	api := renderPackageAPI(pkg)

	expected := []string{
		"const Answer untyped int = 42",
		"func Function(example.test/fixture.Public, ...string) (example.test/fixture.Public, error)",
		"methodset *Alias.Pointer()",
		"methodset *Alias.Value(int) string",
		"methodset *Public.Pointer()",
		"methodset *Public.Value(int) string",
		"methodset Alias.Value(int) string",
		"methodset Contract.Do(string) error",
		"methodset Public.Value(int) string",
		"type Alias = example.test/fixture.Public",
		"type Contract interface{Do(string) error}",
		"type Public struct{Visible int tag \"json:\\\"visible\\\"\"; <private fields present>}",
		"var Current example.test/fixture.Public",
	}
	if !slices.Equal(api.Declarations, expected) {
		t.Fatalf(
			"declarations mismatch\nwant:\n%s\ngot:\n%s",
			strings.Join(expected, "\n"),
			strings.Join(api.Declarations, "\n"),
		)
	}
}

func TestUnexportedStructDetailsDoNotChangeManifest(t *testing.T) {
	t.Parallel()

	first := renderPackageAPI(checkSource(t, `
package fixture
type Public struct {
	Visible int
	hidden string
}
`))
	second := renderPackageAPI(checkSource(t, `
package fixture
type Public struct {
	Visible int
	implementationDetail []byte
}
`))
	if !slices.Equal(first.Declarations, second.Declarations) {
		t.Fatalf("private implementation changed API:\n%v\n%v", first, second)
	}
}

func TestGenericParameterNamesDoNotChangeManifest(t *testing.T) {
	t.Parallel()

	first := renderPackageAPI(checkSource(t, `
package fixture
type Container[Element any] struct { Value Element }
type Lookup[Key comparable, Value any] = map[Key]Value
func Transform[Input any](value Input) Input { return value }
`))
	second := renderPackageAPI(checkSource(t, `
package fixture
type Container[T any] struct { Value T }
type Lookup[K comparable, V any] = map[K]V
func Transform[T any](value T) T { return value }
`))
	if !slices.Equal(first.Declarations, second.Declarations) {
		t.Fatalf("generic parameter rename changed API:\n%v\n%v", first, second)
	}
	if !slices.Contains(
		first.Declarations,
		"type Lookup[T0 comparable, T1 interface{}] = map[T0]T1",
	) {
		t.Fatalf("generic alias parameters missing from API: %v", first.Declarations)
	}
}

func TestCompareRejectsIncompatibleAndAdditiveDrift(t *testing.T) {
	t.Parallel()

	baseline := Manifest{Packages: []PackageAPI{{
		Path:         "example.test/fixture",
		Name:         "fixture",
		Declarations: []string{"func Run(int) error"},
	}}}
	incompatible := Manifest{Packages: []PackageAPI{{
		Path:         "example.test/fixture",
		Name:         "fixture",
		Declarations: []string{"func Run(string) error"},
	}}}
	if err := Compare(baseline, incompatible); err == nil ||
		!strings.Contains(err.Error(), "- example.test/fixture: func Run(int) error") ||
		!strings.Contains(err.Error(), "+ example.test/fixture: func Run(string) error") {
		t.Fatalf("signature drift result = %v", err)
	}

	additive := Manifest{Packages: []PackageAPI{{
		Path: "example.test/fixture",
		Name: "fixture",
		Declarations: []string{
			"func Added()",
			"func Run(int) error",
		},
	}}}
	if err := Compare(baseline, additive); err == nil {
		t.Fatal("additive drift did not require baseline review")
	}
	addedPackage := Manifest{Packages: []PackageAPI{
		baseline.Packages[0],
		{
			Path:         "example.test/newpkg",
			Name:         "newpkg",
			Declarations: []string{"func New()"},
		},
	}}
	if err := Compare(baseline, addedPackage); err == nil ||
		!strings.Contains(err.Error(), "+ package example.test/newpkg") {
		t.Fatalf("new package discovery drift result = %v", err)
	}
	if err := Compare(baseline, baseline); err != nil {
		t.Fatalf("identical manifest rejected: %v", err)
	}

	renamed := baseline
	renamed.Packages = slices.Clone(baseline.Packages)
	renamed.Packages[0].Name = "renamed"
	if err := Compare(baseline, renamed); err == nil ||
		!strings.Contains(err.Error(), "name fixture") ||
		!strings.Contains(err.Error(), "name renamed") {
		t.Fatalf("package-name drift result = %v", err)
	}
}

func TestRenderedManifestRejectsBreakingFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		old  string
		new  string
	}{
		{
			name: "function signature",
			old:  "package fixture\nfunc Run(int) error { return nil }\n",
			new:  "package fixture\nfunc Run(string) error { return nil }\n",
		},
		{
			name: "exported struct field type",
			old:  "package fixture\ntype Public struct { Value int }\n",
			new:  "package fixture\ntype Public struct { Value string }\n",
		},
		{
			name: "interface method",
			old:  "package fixture\ntype Contract interface { Run(int) error }\n",
			new:  "package fixture\ntype Contract interface { Run(string) error }\n",
		},
		{
			name: "exported declaration removal",
			old:  "package fixture\nfunc Removed() {}\n",
			new:  "package fixture\n",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			baseline := Manifest{Packages: []PackageAPI{
				renderPackageAPI(checkSource(t, test.old)),
			}}
			current := Manifest{Packages: []PackageAPI{
				renderPackageAPI(checkSource(t, test.new)),
			}}
			if err := Compare(baseline, current); err == nil {
				t.Fatal("breaking fixture did not change the manifest")
			}
		})
	}
}

func TestManifestRoundTrip(t *testing.T) {
	t.Parallel()

	manifest := Manifest{Packages: []PackageAPI{
		{
			Path:         "example.test/a",
			Name:         "a",
			Declarations: []string{"func A()"},
		},
		{
			Path:         "example.test/b",
			Name:         "b",
			Declarations: []string{"type B int"},
		},
	}}
	parsed, err := ParseManifest(manifest.Render())
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if rendered := parsed.Render(); rendered != manifest.Render() {
		t.Fatalf("round trip mismatch:\n%s", rendered)
	}

	invalid := strings.Replace(
		manifest.Render(),
		"package example.test/a",
		"package example.test/b",
		1,
	)
	if _, err := ParseManifest(invalid); err == nil {
		t.Fatal("unsorted duplicate package accepted")
	}
}

func TestManifestRoundTripAllowsLongDeclarations(t *testing.T) {
	t.Parallel()

	manifest := Manifest{Packages: []PackageAPI{{
		Path:         "example.test/fixture",
		Name:         "fixture",
		Declarations: []string{"type Large " + strings.Repeat("x", 128*1024)},
	}}}
	parsed, err := ParseManifest(manifest.Render())
	if err != nil {
		t.Fatalf("ParseManifest long declaration: %v", err)
	}
	if parsed.Render() != manifest.Render() {
		t.Fatal("long declaration changed during round trip")
	}
}

func checkSource(t *testing.T, source string) *types.Package {
	t.Helper()

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "fixture.go", source, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	config := types.Config{}
	pkg, err := config.Check(
		"example.test/fixture",
		fileSet,
		[]*ast.File{file},
		nil,
	)
	if err != nil {
		t.Fatalf("type-check fixture: %v", err)
	}
	return pkg
}
