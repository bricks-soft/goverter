package generator

import (
	"fmt"
	"go/types"
	"sort"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/jmattheis/go-genconv/builder"
	"github.com/jmattheis/go-genconv/comments"
	"github.com/jmattheis/go-genconv/namer"
)

type Method struct {
	ID            string
	Name          string
	Source        types.Type
	Target        types.Type
	Mapping       map[string]string
	IgnoredFields map[string]struct{}
	Delegate      *types.Func
}

type Signature struct {
	Source string
	Target string
}

type Generator struct {
	namer  *namer.Namer
	name   string
	file   *jen.File
	lookup map[Signature]*Method
}

func (g *Generator) registerMethod(sources *types.Package, method *types.Func, methodComments comments.Method) error {
	signature, ok := method.Type().(*types.Signature)
	if !ok {
		return fmt.Errorf("expected signature %#v", method.Type())
	}
	params := signature.Params()
	if params.Len() != 1 {
		return fmt.Errorf("expected signature to have only one parameter")
	}
	result := signature.Results()
	if result.Len() != 1 {
		return fmt.Errorf("expected signature to have only one parameter")
	}
	source := params.At(0).Type()
	target := result.At(0).Type()

	m := &Method{
		ID:            method.FullName(),
		Name:          method.Name(),
		Source:        source,
		Target:        target,
		Mapping:       methodComments.NameMapping,
		IgnoredFields: methodComments.IgnoredFields,
	}

	if methodComments.Delegate != "" {
		delegate := sources.Scope().Lookup(methodComments.Delegate)
		if delegate == nil {
			return fmt.Errorf("delegate %s does not exist", methodComments.Delegate)
		}
		if f, ok := delegate.(*types.Func); ok {
			m.Delegate = f
		} else {
			return fmt.Errorf("delegate %s does is not a function", methodComments.Delegate)
		}
	}

	g.lookup[Signature{
		Source: source.String(),
		Target: target.String(),
	}] = m
	g.namer.Register(m.Name)
	return nil
}

func (g *Generator) createMethods() error {
	methods := []*Method{}
	for _, method := range g.lookup {
		methods = append(methods, method)
	}
	sort.Slice(methods, func(i, j int) bool {
		return methods[i].Name < methods[j].Name
	})
	for _, method := range methods {
		err := g.addMethod(method)
		if err != nil {
			err = err.Lift(&builder.Path{
				SourceID:   "source",
				TargetID:   "target",
				SourceType: method.Source.String(),
				TargetType: method.Target.String(),
			})
			return fmt.Errorf("Error while creating converter method: %s\n\n%s", method.ID, builder.ToString(err))
		}
	}
	return nil
}

func (g *Generator) addMethod(method *Method) *builder.Error {
	sourceID := jen.Id("source")
	source := builder.TypeOf(method.Source)
	target := builder.TypeOf(method.Target)

	if method.Delegate != nil {
		g.file.Func().Params(jen.Id("c").Op("*").Id(g.name)).Id(method.Name).
			Params(jen.Id("source").Add(source.TypeAsJen())).Params(target.TypeAsJen()).
			Block(jen.Return(jen.Qual(method.Delegate.Pkg().Path(), method.Delegate.Name())).Call(jen.Id("c"), sourceID))
		return nil
	}

	stmt, newID, err := g.BuildNoLookup(&builder.MethodContext{
		Namer:         namer.New(),
		MappingBaseID: target.T.String(),
		Mapping:       method.Mapping,
		IgnoredFields: method.IgnoredFields,
	}, builder.VariableID(sourceID.Clone()), source, target)
	if err != nil {
		return err
	}

	stmt = append(stmt, jen.Return().Add(newID.Code))
	g.file.Func().Params(jen.Id("c").Op("*").Id(g.name)).Id(method.Name).
		Params(jen.Id("source").Add(source.TypeAsJen())).Params(target.TypeAsJen()).
		Block(stmt...)

	return nil
}

func (g *Generator) BuildNoLookup(ctx *builder.MethodContext, sourceID *builder.JenID, source, target *builder.Type) ([]jen.Code, *builder.JenID, *builder.Error) {
	for _, rule := range BuildSteps {
		if rule.Matches(source, target) {
			return rule.Build(g, ctx, sourceID, source, target)
		}
	}
	return nil, nil, builder.NewError(fmt.Sprintf("TypeMismatch: Cannot convert %s to %s", source.T, target.T))
}

func (g *Generator) Build(ctx *builder.MethodContext, sourceID *builder.JenID, source, target *builder.Type) ([]jen.Code, *builder.JenID, *builder.Error) {
	if method, ok := g.lookup[Signature{Source: source.T.String(), Target: target.T.String()}]; ok {
		id := builder.OtherID(jen.Id("c").Dot(method.Name).Call(sourceID.Code))
		return nil, id, nil
	}

	if (source.Named && !source.Basic) || (target.Named && !target.Basic) {
		name := g.namer.Name(source.UnescapedID() + "To" + strings.Title(target.UnescapedID()))

		method := &Method{
			ID:            name,
			Name:          name,
			Source:        source.T,
			Target:        target.T,
			Mapping:       map[string]string{},
			IgnoredFields: map[string]struct{}{},
		}
		g.lookup[Signature{Source: source.T.String(), Target: target.T.String()}] = method
		g.namer.Register(method.Name)
		if err := g.addMethod(method); err != nil {
			return nil, nil, err
		}
		// try again to trigger the found method thingy above
		return g.Build(ctx, sourceID, source, target)
	}

	for _, rule := range BuildSteps {
		if rule.Matches(source, target) {
			return rule.Build(g, ctx, sourceID, source, target)
		}
	}

	return nil, nil, builder.NewError(fmt.Sprintf("TypeMismatch: Cannot convert %s to %s", source.T, target.T))
}
