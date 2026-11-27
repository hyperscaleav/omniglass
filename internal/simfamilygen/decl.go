package simfamilygen

import "go/ast"

// fn is the shape of a func declaration the scan cares about.
type fn struct {
	Name    string
	Recv    *ast.FieldList
	Params  int
	Results int
}

func declFunc(d ast.Decl) (fn, bool) {
	fd, ok := d.(*ast.FuncDecl)
	if !ok {
		return fn{}, false
	}
	out := fn{Name: fd.Name.Name, Recv: fd.Recv}
	if fd.Type.Params != nil {
		out.Params = fd.Type.Params.NumFields()
	}
	if fd.Type.Results != nil {
		out.Results = fd.Type.Results.NumFields()
	}
	return out, true
}
