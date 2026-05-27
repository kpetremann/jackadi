package database

import (
	"github.com/kpetremann/jackadi/internal/node"
	"github.com/kpetremann/jackadi/internal/proto"
)

const (
	ResultKeyPrefix  = "res"
	RequestKeyPrefix = "req"
)

type Task struct {
	Node   node.ID
	Result *proto.TaskResponse
}

type Request struct {
	Task               string
	ConnectedTarget    []string
	DisconnectedTarget []string
}

type Key struct {
	Prefix string
	ID     string
}
