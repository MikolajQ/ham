package core

type VariableType int

const (
	VARIABLE_TYPE_VALUE VariableType = iota
	VARIABLE_TYPE_FILE_PATH
	VARIABLE_TYPE_SECRET
)

type Variable struct {
	Value string
	Type  VariableType
}

type Variables struct {
	Vars map[string]Variable
}

func NewVariables() *Variables {
	return &Variables{
		Vars: make(map[string]Variable),
	}
}

func (ctx *Variables) GetVar(key string) Variable {
	return ctx.Vars[key]
}

func (ctx *Variables) PutVar(key string, value string, ty VariableType) {
	ctx.Vars[key] = Variable{
		Value: value,
		Type:  ty,
	}
}

// Commit of the running client, set by main. Used to check that the
// build server runs a ham-build from the same commit.
var ClientCommit = "Unknown"
