package codegen

import (
	"context"

	"github.com/moby/buildkit/client/llb"
	"github.com/openllb/hlb/checker"
	"github.com/openllb/hlb/parser"
	"github.com/openllb/hlb/solver"
	"github.com/pkg/errors"
)

func (cg *CodeGen) EmitFuncDecl(ctx context.Context, scope *parser.Scope, caller parser.Node, fun *parser.FuncDecl, args []*parser.Expr, chainStart interface{}) (interface{}, error) {
	cg.stacktrace = append(cg.stacktrace, Frame{Node: caller})
	defer func() {
		cg.stacktrace = cg.stacktrace[:len(cg.stacktrace)-1]
	}()

	nonVariadicArgs := 0
	for _, field := range fun.Params.List {
		if field.Variadic == nil {
			nonVariadicArgs++
		}
	}
	if len(args) < nonVariadicArgs {
		return nil, errors.WithStack(errors.Errorf("%s expected args %s, found %s", fun.Name, fun.Params.List, args))
	}

	err := cg.ParameterizedScope(ctx, scope, fun, args)
	if err != nil {
		return nil, err
	}

	// Before executing a function.
	err = cg.Debug(ctx, fun.Scope, fun, chainStart)
	if err != nil {
		return chainStart, err
	}

	switch fun.Type.Primary() {
	case parser.Filesystem:
		return cg.EmitFilesystemBlock(ctx, fun.Scope, fun.Body, chainStart)
	case parser.Option:
		return cg.EmitOptionBlock(ctx, fun.Scope, string(fun.Type.Secondary()), fun.Body)
	case parser.Str:
		return cg.EmitStringBlock(ctx, fun.Scope, fun.Body, chainStart)
	case parser.Group:
		return cg.EmitGroupBlock(ctx, fun.Scope, fun.Body, chainStart)
	default:
		return chainStart, checker.ErrInvalidTarget{Node: fun}
	}
}

func (cg *CodeGen) EmitOptionFuncDecl(ctx context.Context, scope *parser.Scope, caller parser.Node, fun *parser.FuncDecl, args []*parser.Expr) (opts []interface{}, err error) {
	v, err := cg.EmitFuncDecl(ctx, scope, caller, fun, args, nil)
	if err != nil {
		return
	}

	opts, ok := v.([]interface{})
	if !ok {
		return opts, errors.WithStack(ErrCodeGen{fun, ErrBadCast})
	}
	return
}

func (cg *CodeGen) ParameterizedScope(ctx context.Context, scope *parser.Scope, fun *parser.FuncDecl, args []*parser.Expr) error {
	for i, field := range fun.Params.List {
		var (
			data interface{}
			err  error
		)

		kind := field.Type.Primary()
		switch kind {
		case parser.Str:
			var v string
			v, err = cg.EmitStringExpr(ctx, scope, args[i])
			data = v
		case parser.Int:
			var v int
			v, err = cg.EmitIntExpr(ctx, scope, args[i])
			data = v
		case parser.Bool:
			var v bool
			v, err = cg.EmitBoolExpr(ctx, scope, args[i])
			data = v
		case parser.Filesystem:
			var v llb.State
			v, err = cg.EmitFilesystemExpr(ctx, scope, args[i])
			data = v
		case parser.Option:
			var v []interface{}
			if field.Variadic != nil {
				for j := i; j < len(args); j++ {
					var vv []interface{}
					vv, err = cg.EmitOptionExpr(ctx, scope, args[i], nil, string(field.Type.Secondary()))
					if err != nil {
						break
					}
					v = append(v, vv...)
				}
			} else {
				v, err = cg.EmitOptionExpr(ctx, scope, args[i], nil, string(field.Type.Secondary()))
			}
			data = v
		case parser.Group:
			var v solver.Request
			v, err = cg.EmitGroupExpr(ctx, scope, args[i])
			data = v
		}
		if err != nil {
			return ErrCodeGen{Node: args[i], Err: err}
		}

		fun.Scope.Insert(&parser.Object{
			Kind:  parser.ExprKind,
			Ident: field.Name,
			Node:  field,
			Data:  data,
		})
	}
	return nil
}
