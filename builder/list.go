package builder

import (
	"github.com/dave/jennifer/jen"
	"github.com/jmattheis/goverter/xtype"
)

// List handles array / slice types.
type List struct{}

// Matches returns true, if the builder can create handle the given types.
func (*List) Matches(source, target *xtype.Type) bool {
	return source.List && target.List && !target.ListFixed
}

// Build creates conversion source code for the given source and target type.
func (*List) Build(gen Generator, ctx *MethodContext, sourceID *xtype.JenID, source, target *xtype.Type) ([]jen.Code, *xtype.JenID, *Error) {
	targetSlice := ctx.Name(target.ID())
	index := ctx.Index()

	indexedSource := xtype.VariableID(sourceID.Code.Clone().Index(jen.Id(index)))

	errWrapper := Wrap("error setting index %d", jen.Id(index))
	newStmt, newID, err := gen.Build(ctx, indexedSource, source.ListInner, target.ListInner, errWrapper)
	if err != nil {
		return nil, nil, err.Lift(&Path{
			SourceID:   "[]",
			SourceType: source.ListInner.T.String(),
			TargetID:   "[]",
			TargetType: target.ListInner.T.String(),
		})
	}
	newStmt = append(newStmt, jen.Id(targetSlice).Index(jen.Id(index)).Op("=").Add(newID.Code))

	var initializeStmt []jen.Code
	if source.ListFixed {
		initializeStmt = []jen.Code{
			jen.Id(targetSlice).Op(":=").Make(target.TypeAsJen(), jen.Len(sourceID.Code.Clone()), jen.Len(sourceID.Code.Clone())),
		}
	} else {
		initializeStmt = []jen.Code{
			jen.Var().Add(jen.Id(targetSlice), target.TypeAsJen()),
			jen.If(sourceID.Code.Clone().Op("!=").Nil()).Block(
				jen.Id(targetSlice).Op("=").Make(target.TypeAsJen(), jen.Len(sourceID.Code.Clone()), jen.Len(sourceID.Code.Clone())),
			),
		}
	}

	loopAssignStmt := jen.For(jen.Id(index).Op(":=").Lit(0), jen.Id(index).Op("<").Len(sourceID.Code.Clone()), jen.Id(index).Op("++")).
		Block(newStmt...)

	stmt := append(initializeStmt, loopAssignStmt)

	return stmt, xtype.VariableID(jen.Id(targetSlice)), nil
}
