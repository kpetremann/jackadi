package sdk

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"reflect"
	"strings"

	"github.com/kpetremann/jackadi/internal/plugin/core"
	"github.com/kpetremann/jackadi/internal/proto"
	"github.com/kpetremann/jackadi/internal/serializer"
)

// LockMode represents the locking behavior for task execution.
type LockMode int

const (
	// NoLock allows concurrent execution without any locking restrictions (default).
	NoLock LockMode = iota
	// WriteLock allows only one write task at a time, but read tasks can run concurrently.
	WriteLock
	// ExclusiveLock ensures only one task runs at a time, blocking all others.
	// Be careful, it blocks any other tasks like specs refresh.
	// Usually for Jackadi internal operations.
	ExclusiveLock
)

func (lm LockMode) String() string {
	switch lm {
	case NoLock:
		return "no-lock"
	case WriteLock:
		return "write"
	case ExclusiveLock:
		return "exclusive"
	default:
		return "unknown"
	}
}

// toProtoLockMode converts SDK LockMode to proto LockMode.
func (lm LockMode) toProtoLockMode() proto.LockMode {
	switch lm {
	case NoLock:
		return proto.LockMode_NO_LOCK
	case WriteLock:
		return proto.LockMode_WRITE
	case ExclusiveLock:
		return proto.LockMode_EXCLUSIVE
	default:
		return proto.LockMode_UNSPECIFIED
	}
}

type args struct {
	Name    string
	Type    string
	Example string
}

type Task struct {
	function    any
	name        string
	summary     string
	description string
	flags       []Flag
	args        []args
	lockMode    LockMode
}

// WithSummary set the short description.
func (t *Task) WithSummary(text string) *Task {
	t.summary = text
	return t
}

// WithDescription set long description.
func (t *Task) WithDescription(text string) *Task {
	t.description = text
	return t
}

// WithDoc add an argument.
func (t *Task) WithArg(name, argType, example string) *Task {
	t.args = append(t.args, args{name, argType, example})
	return t
}

// WithFlags add flags (e.g. Deprecated, NotImplemented, ...).
func (t *Task) WithFlags(flags ...Flag) *Task {
	t.flags = append(t.flags, flags...)
	return t
}

// WithLockMode sets the default lock mode for this task.
func (t *Task) WithLockMode(lockMode LockMode) *Task {
	t.lockMode = lockMode
	return t
}

// getLockMode returns the default lock mode for this task.
func (t *Task) getLockMode() LockMode {
	return t.lockMode
}

func (t Task) flagsString() string {
	strFlags := make([]string, len(t.flags))
	for i, f := range t.flags {
		strFlags[i] = string(f)
	}
	return strings.Join(strFlags, ", ")
}

func (t *Task) helpText(pluginName string) string {
	var sb strings.Builder

	if t.summary != "" {
		sb.WriteString("Summary:\n")
		sb.WriteString(indentMultiline(t.summary, "  "))
		sb.WriteString("\n\n")
	}

	if t.description != "" {
		sb.WriteString("Description:\n")
		sb.WriteString(indentMultiline(t.description, "  "))
		sb.WriteString("\n\n")
	}

	if len(t.args) > 0 {
		fmt.Fprintf(&sb, "Usage:\n  jack run <target> %s:%s", pluginName, t.name)
		for _, arg := range t.args {
			fmt.Fprintf(&sb, " <%s>", arg.Name)
		}
		sb.WriteString("\n\n")
	}

	if len(t.args) > 0 {
		sb.WriteString("Arguments:\n")
		for _, arg := range t.args {
			fmt.Fprintf(&sb, "%-12s %-8s e.g. %s\n", arg.Name, arg.Type, arg.Example)
		}
		sb.WriteString("\n")
	}

	if len(t.flags) > 0 {
		sb.WriteString("Flags:\n")
		flagNames := make([]string, 0, len(t.flags))
		for _, f := range t.flags {
			flagNames = append(flagNames, string(f))
		}
		fmt.Fprintf(&sb, "  %s\n\n", strings.Join(flagNames, ", "))
	}

	if t.lockMode != NoLock {
		fmt.Fprintf(&sb, "Lock Mode: %s\n\n", t.lockMode.String())
	}

	return sb.String()
}

func indentMultiline(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// MustRegisterTask registers a task (function) with a string identifier.
func (t *Plugin) MustRegisterTask(name string, function any) *Task {
	funcValue := reflect.ValueOf(function)
	funcType := funcValue.Type()

	contextType := reflect.TypeFor[context.Context]()
	optionsType := reflect.TypeFor[Options]()

	if funcType.NumOut() != 2 {
		log.Fatalln("function must return at least one value and an error")
	}

	errorType := reflect.TypeFor[error]()
	if !funcType.Out(funcType.NumOut() - 1).Implements(errorType) {
		log.Fatalln("last return value of the task must be an error")
	}

	// Check that input types are supported
	for i := 2; i < funcType.NumIn(); i++ {
		paramType := funcType.In(i)

		switch {
		case paramType.Implements(contextType):
		case paramType.Implements(optionsType):
		}

		// Handle pointer types
		if paramType.Kind() == reflect.Pointer {
			paramType = paramType.Elem()
		}

		switch paramType.Kind() { //nolint:exhaustive  //we do not support all types
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64, reflect.Bool, reflect.String,
			reflect.Struct, reflect.Slice, reflect.Array, reflect.Map, reflect.Interface:
			// These types are supported
		default:
			log.Fatalf("unsupported parameter type at position %d: %v", i, paramType)
		}
	}

	t.tasks[name] = &Task{function: function, name: name, lockMode: NoLock}
	t.taskNames = append(t.taskNames, name)

	return t.tasks[name]
}

func (t Plugin) Do(ctx context.Context, task string, input *proto.Input) (core.Response, error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("recovered from panic", "plugin", t.name, "error", r)
		}
	}()

	if input == nil {
		return core.Response{}, errors.New("internal error: proto input args cannot be nil")
	}

	selectedTask, ok := t.tasks[task]
	if !ok {
		return core.Response{
			Output:  nil,
			Error:   "",
			Retcode: -1,
		}, fmt.Errorf("unknown task '%s' in plugin '%s'", task, t.name)
	}

	// reflect to ensure to call the function with params defined in the signature
	function := selectedTask.function
	funcValue := reflect.ValueOf(function)
	funcType := funcValue.Type()

	inputs, err := handleInputs(ctx, funcType, input)
	if err != nil {
		return core.Response{}, err
	}

	// call the task
	ret := funcValue.Call(inputs)

	// parse return
	taskOut, taskErr, err := parseReturn(ret)
	if err != nil {
		return core.Response{}, err
	}

	return core.Response{
		Output: taskOut,
		Error:  taskErr,
	}, nil
}

func parseReturn(ret []reflect.Value) ([]byte, string, error) {
	if len(ret) < 2 {
		return nil, "", errors.New("function returned insufficient values")
	}
	if !ret[0].IsValid() {
		return nil, "", errors.New("function returned invalid first value")
	}

	out := ret[0].Interface()
	taskErr := ""
	if ret[1].IsValid() && ret[1].Interface() != nil {
		err, ok := ret[1].Interface().(error)

		if !ok {
			return nil, "", errors.New("function returned invalid error")
		}
		if err != nil {
			taskErr = err.Error()
		}
	}

	taskOut := []byte{}
	if out == nil {
		return taskOut, taskErr, nil
	}

	// dereferencing if the return is a pointer
	if reflect.TypeOf(out).Kind() == reflect.Pointer {
		valOut := reflect.ValueOf(out)
		if !valOut.IsValid() || valOut.IsNil() {
			out = nil
		} else {
			out = valOut.Elem().Interface()
		}
	}

	// we serialize with jsoniter+UserNumber to avoid losing int64/uint64 precision
	// if we use protobuf Value, numeric precision would be lost as they stored as a float64.
	taskOut, err := serializer.JSON.Marshal(out)
	if err != nil {
		return nil, "", fmt.Errorf("unable to serialize task result: %w", err)
	}

	return taskOut, taskErr, nil
}

func handleInputs(ctx context.Context, funcType reflect.Type, input *proto.Input) ([]reflect.Value, error) {
	if input.Args == nil {
		return nil, errors.New("input.Args must not be empty")
	}
	inputs := []reflect.Value{}

	// offset indicates at which position starts the args of the targeted function
	// as context and option are optional but must be placed in 1st and 2nd position if both exist
	// or, 1st position if only one of them exists.
	//
	// e.g.:
	// func F1(arg1, arg2 string) => offset == 0
	// func F2(ctx context.Context, arg1, arg2 string) => offset == 1
	// func F3(ctx context.Context, options Options, arg1, arg2 string) => offset == 3
	offset := 0

	// handle context
	contextType := reflect.TypeFor[context.Context]()
	if offset < funcType.NumIn() && funcType.In(offset).Implements(contextType) {
		offset++
		inputs = append(inputs, reflect.ValueOf(ctx))
	}

	// handle options
	optionsType := reflect.TypeFor[Options]()
	if offset < funcType.NumIn() && funcType.In(offset).Implements(optionsType) {
		opts, err := handleOptions(funcType.In(offset).Elem(), input)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, reflect.ValueOf(opts.Elem().Addr().Interface()))
		offset++
	}

	// fails if number of arguments does not match
	// expected arguments are the number of parameters in the function minus the context and option structs
	if len(input.Args.Values) != funcType.NumIn()-offset {
		return nil, fmt.Errorf("expected number of arguments: %d, got: %d", funcType.NumIn()-offset, len(input.Args.Values))
	}

	// convert all arguments to expected type of task parameters
	for i, in := range input.Args.Values {
		if reflect.TypeOf(in.AsInterface()) == funcType.In(i+offset) {
			inputs = append(inputs, reflect.ValueOf(in.AsInterface()))
			continue
		}

		out, err := core.StructpbValueToInput(in.AsInterface(), funcType.In(i+offset))
		if err != nil {
			log.Println(err)
			return nil, fmt.Errorf("unable to convert argument n°%d to %s", i, funcType.In(i+offset))
		}
		inputs = append(inputs, reflect.ValueOf(out))
	}
	return inputs, nil
}

// findField finds a struct field by its jackadi tag or field name.
func findField(structValue reflect.Value, key string) (reflect.Value, string, bool) {
	structType := structValue.Type()

	// handle jackadi tag
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)

		if tag, ok := field.Tag.Lookup("jackadi"); ok {
			parts := strings.Split(tag, ",")
			if len(parts) == 0 {
				continue
			}
			tagName := parts[0] // handle tags like "field_name,omitempty"
			if tagName == key {
				return structValue.Field(i), field.Name, true
			}
		}
	}

	// no jackadi tag: fallback to natural field name
	field := structValue.FieldByName(key)
	if field.IsValid() {
		return field, key, true
	}

	return reflect.Value{}, "", false
}

func handleOptions(optionElemType reflect.Type, input *proto.Input) (reflect.Value, error) {
	opts := reflect.New(optionElemType)
	op, ok := opts.Interface().(Options)
	if !ok {
		return reflect.Value{}, errors.New("input not implementing sdk.Option properly")
	}
	op.SetDefaults()

	// no option passed in input
	if input.GetOptions() == nil {
		return opts, nil
	}

	// handle Options argument of the targeted fuction
	for k := range input.GetOptions().AsMap() {
		field, fieldName, found := findField(opts.Elem(), k)
		if !found || !field.CanSet() {
			return reflect.Value{}, fmt.Errorf("invalid '%s' option", k)
		}

		res, err := core.StructpbValueToInput(input.Options.Fields[k].AsInterface(), field.Type())
		if err != nil {
			return reflect.Value{}, fmt.Errorf("unable to convert '%s' option to '%s' (field: %s)", k, field.Type(), fieldName)
		}
		field.Set(reflect.ValueOf(res))
	}

	return opts, nil
}
