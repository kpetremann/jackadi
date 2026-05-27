package task

import (
	"github.com/kpetremann/jackadi/internal/proto"
)

type Target struct {
	Exact  bool
	List   bool
	File   bool
	Glob   bool
	Regexp bool
	Query  bool
}

func (t Target) Mode() proto.TargetMode {
	switch {
	case t.Exact:
		return proto.TargetMode_EXACT
	case t.List, t.File:
		return proto.TargetMode_LIST
	case t.Glob:
		return proto.TargetMode_GLOB
	case t.Regexp:
		return proto.TargetMode_REGEX
	case t.Query:
		return proto.TargetMode_QUERY
	}
	return proto.TargetMode_GLOB
}
