package apicompat

import (
	"fmt"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

func renderPackageAPI(pkg *types.Package) PackageAPI {
	renderer := typeRenderer{}
	scope := pkg.Scope()
	names := scope.Names()
	slices.Sort(names)

	declarations := make([]string, 0, len(names))
	for _, name := range names {
		if !token.IsExported(name) {
			continue
		}
		object := scope.Lookup(name)
		declarations = append(declarations, renderer.renderObject(object)...)
	}
	slices.Sort(declarations)
	return PackageAPI{
		Path:         pkg.Path(),
		Name:         pkg.Name(),
		Declarations: declarations,
	}
}

type typeRenderer struct{}

func (renderer typeRenderer) renderObject(object types.Object) []string {
	switch object := object.(type) {
	case *types.Const:
		return []string{fmt.Sprintf(
			"const %s %s = %s",
			object.Name(),
			renderer.renderType(object.Type()),
			object.Val().ExactString(),
		)}
	case *types.Var:
		return []string{fmt.Sprintf(
			"var %s %s",
			object.Name(),
			renderer.renderType(object.Type()),
		)}
	case *types.Func:
		signature, ok := object.Type().(*types.Signature)
		if !ok {
			return []string{"func " + object.Name() + " <invalid signature>"}
		}
		return []string{
			"func " + object.Name() + renderer.renderSignature(signature),
		}
	case *types.TypeName:
		return renderer.renderTypeName(object)
	default:
		return []string{
			fmt.Sprintf("%s %s", object.Name(), renderer.renderType(object.Type())),
		}
	}
}

func (renderer typeRenderer) renderTypeName(object *types.TypeName) []string {
	if object.IsAlias() {
		target := types.Unalias(object.Type())
		var parameters *types.TypeParamList
		if alias, ok := object.Type().(*types.Alias); ok {
			target = alias.Rhs()
			parameters = alias.TypeParams()
		}
		declarations := []string{
			"type " + object.Name() +
				renderer.renderTypeParameters(parameters) +
				" = " + renderer.renderType(target),
		}
		declarations = append(
			declarations,
			renderer.renderMethodSet(object.Name(), object.Type(), false)...,
		)
		declarations = append(
			declarations,
			renderer.renderMethodSet(
				object.Name(),
				types.NewPointer(object.Type()),
				true,
			)...,
		)
		return declarations
	}

	named, ok := object.Type().(*types.Named)
	if !ok {
		return []string{
			"type " + object.Name() + " " + renderer.renderType(object.Type()),
		}
	}

	declarations := []string{
		"type " + object.Name() +
			renderer.renderTypeParameters(named.TypeParams()) +
			" " + renderer.renderType(named.Underlying()),
	}
	declarations = append(
		declarations,
		renderer.renderMethodSet(object.Name(), named, false)...,
	)
	declarations = append(
		declarations,
		renderer.renderMethodSet(object.Name(), types.NewPointer(named), true)...,
	)
	return declarations
}

func (renderer typeRenderer) renderMethodSet(
	typeName string,
	typ types.Type,
	pointer bool,
) []string {
	methodSet := types.NewMethodSet(typ)
	if methodSet.Len() == 0 {
		return nil
	}

	receiver := typeName
	if pointer {
		receiver = "*" + receiver
	}
	methods := make([]string, 0, methodSet.Len())
	for index := 0; index < methodSet.Len(); index++ {
		method, ok := methodSet.At(index).Obj().(*types.Func)
		if !ok || !method.Exported() {
			continue
		}
		signature, ok := method.Type().(*types.Signature)
		if !ok {
			continue
		}
		methods = append(
			methods,
			"methodset "+receiver+"."+method.Name()+
				renderer.renderSignature(signature),
		)
	}
	return methods
}

func (renderer typeRenderer) renderType(typ types.Type) string {
	switch typ := typ.(type) {
	case *types.Basic:
		return typ.Name()
	case *types.Array:
		return fmt.Sprintf("[%d]%s", typ.Len(), renderer.renderType(typ.Elem()))
	case *types.Slice:
		return "[]" + renderer.renderType(typ.Elem())
	case *types.Struct:
		return renderer.renderStruct(typ)
	case *types.Pointer:
		return "*" + renderer.renderType(typ.Elem())
	case *types.Tuple:
		return renderer.renderTuple(typ, false)
	case *types.Signature:
		return "func" + renderer.renderSignature(typ)
	case *types.Interface:
		return renderer.renderInterface(typ)
	case *types.Map:
		return "map[" + renderer.renderType(typ.Key()) + "]" +
			renderer.renderType(typ.Elem())
	case *types.Chan:
		switch typ.Dir() {
		case types.SendOnly:
			return "chan<- " + renderer.renderType(typ.Elem())
		case types.RecvOnly:
			return "<-chan " + renderer.renderType(typ.Elem())
		default:
			return "chan " + renderer.renderType(typ.Elem())
		}
	case *types.Named:
		return renderer.renderNamed(typ)
	case *types.Alias:
		return renderer.renderType(typ.Rhs())
	case *types.TypeParam:
		return renderer.renderTypeParameterName(typ)
	case *types.Union:
		return renderer.renderUnion(typ)
	default:
		return types.TypeString(typ, fullPackageQualifier)
	}
}

func (renderer typeRenderer) renderNamed(named *types.Named) string {
	object := named.Obj()
	name := object.Name()
	if object.Pkg() != nil {
		name = object.Pkg().Path() + "." + name
	}
	if named.TypeArgs() == nil || named.TypeArgs().Len() == 0 {
		return name
	}

	arguments := make([]string, 0, named.TypeArgs().Len())
	for index := 0; index < named.TypeArgs().Len(); index++ {
		arguments = append(arguments, renderer.renderType(named.TypeArgs().At(index)))
	}
	return name + "[" + strings.Join(arguments, ", ") + "]"
}

func (renderer typeRenderer) renderStruct(structType *types.Struct) string {
	fields := make([]string, 0, structType.NumFields()+1)
	hasPrivate := false
	for index := 0; index < structType.NumFields(); index++ {
		field := structType.Field(index)
		if !field.Exported() {
			hasPrivate = true
			continue
		}

		var rendered string
		if field.Embedded() {
			rendered = "embedded " + field.Name() + " " +
				renderer.renderType(field.Type())
		} else {
			rendered = field.Name() + " " + renderer.renderType(field.Type())
		}
		if tag := structType.Tag(index); tag != "" {
			rendered += " tag " + strconv.Quote(tag)
		}
		fields = append(fields, rendered)
	}
	if hasPrivate {
		fields = append(fields, "<private fields present>")
	}
	return "struct{" + strings.Join(fields, "; ") + "}"
}

func (renderer typeRenderer) renderInterface(interfaceType *types.Interface) string {
	interfaceType = interfaceType.Complete()
	elements := make([]string, 0, interfaceType.NumMethods()+interfaceType.NumEmbeddeds())
	for index := 0; index < interfaceType.NumMethods(); index++ {
		method := interfaceType.Method(index)
		name := method.Name()
		if !method.Exported() && method.Pkg() != nil {
			name = method.Pkg().Path() + "." + name
		}
		signature, ok := method.Type().(*types.Signature)
		if !ok {
			elements = append(elements, name+" <invalid signature>")
			continue
		}
		elements = append(elements, name+renderer.renderSignature(signature))
	}
	for index := 0; index < interfaceType.NumEmbeddeds(); index++ {
		elements = append(
			elements,
			"embedded "+renderer.renderType(interfaceType.EmbeddedType(index)),
		)
	}
	slices.Sort(elements)
	return "interface{" + strings.Join(elements, "; ") + "}"
}

func (renderer typeRenderer) renderUnion(union *types.Union) string {
	terms := make([]string, 0, union.Len())
	for index := 0; index < union.Len(); index++ {
		term := union.Term(index)
		rendered := renderer.renderType(term.Type())
		if term.Tilde() {
			rendered = "~" + rendered
		}
		terms = append(terms, rendered)
	}
	return strings.Join(terms, " | ")
}

func (renderer typeRenderer) renderSignature(signature *types.Signature) string {
	return renderer.renderTypeParameters(signature.TypeParams()) +
		renderer.renderTuple(signature.Params(), signature.Variadic()) +
		renderer.renderResults(signature.Results())
}

func (renderer typeRenderer) renderTypeParameters(parameters *types.TypeParamList) string {
	if parameters == nil || parameters.Len() == 0 {
		return ""
	}
	rendered := make([]string, 0, parameters.Len())
	for index := 0; index < parameters.Len(); index++ {
		parameter := parameters.At(index)
		rendered = append(
			rendered,
			renderer.renderTypeParameterName(parameter)+" "+
				renderer.renderType(parameter.Constraint()),
		)
	}
	return "[" + strings.Join(rendered, ", ") + "]"
}

func (typeRenderer) renderTypeParameterName(parameter *types.TypeParam) string {
	if index := parameter.Index(); index >= 0 {
		return fmt.Sprintf("T%d", index)
	}
	return "T"
}

func (renderer typeRenderer) renderTuple(tuple *types.Tuple, variadic bool) string {
	if tuple == nil || tuple.Len() == 0 {
		return "()"
	}
	values := make([]string, 0, tuple.Len())
	for index := 0; index < tuple.Len(); index++ {
		typ := tuple.At(index).Type()
		if variadic && index == tuple.Len()-1 {
			if slice, ok := typ.(*types.Slice); ok {
				values = append(values, "..."+renderer.renderType(slice.Elem()))
				continue
			}
		}
		values = append(values, renderer.renderType(typ))
	}
	return "(" + strings.Join(values, ", ") + ")"
}

func (renderer typeRenderer) renderResults(results *types.Tuple) string {
	if results == nil || results.Len() == 0 {
		return ""
	}
	if results.Len() == 1 {
		return " " + renderer.renderType(results.At(0).Type())
	}
	return " " + renderer.renderTuple(results, false)
}

func fullPackageQualifier(pkg *types.Package) string {
	if pkg == nil {
		return ""
	}
	return pkg.Path()
}
