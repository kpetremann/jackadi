package sdk

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"reflect"

	"github.com/kpetremann/jackadi/internal/serializer"
)

type SpecCollector struct {
	function    any
	name        string
	summary     string
	description string
	flags       []Flag
}

// WithSummary set the short description.
func (p *SpecCollector) WithSummary(text string) *SpecCollector {
	p.summary = text
	return p
}

// WithDescription set long description.
func (p *SpecCollector) WithDescription(text string) *SpecCollector {
	p.description = text
	return p
}

// WithFlags add flags (e.g. Deprecated, NotImplemented, ...).
func (p *SpecCollector) WithFlags(flags ...Flag) *SpecCollector {
	p.flags = append(p.flags, flags...)
	return p
}

// MustRegister registers a task (function) with a string identifier.
func (t *Plugin) MustRegisterSpecCollector(name string, function any) *SpecCollector {
	funcValue := reflect.ValueOf(function)
	funcType := funcValue.Type()

	if funcType.Kind() != reflect.Func {
		log.Fatal("spec task is not a function")
	}

	if funcType.NumIn() > 1 {
		log.Fatalln("spec task must have zero or one input (context)")
	}

	if funcType.NumOut() != 2 {
		log.Fatalln("spec task must return: (struct|map, error)")
	}

	kind := funcType.Out(0).Kind()
	if kind != reflect.Map && kind != reflect.Struct {
		log.Fatalln("spec task first returned value must be of type: 'struct' or 'map'")
	}

	if !funcType.Out(1).Implements(reflect.TypeFor[error]()) {
		log.Fatalln("spec function second returned value must be of type: 'error'")
	}

	t.specs[name] = &SpecCollector{function: function, name: name}
	t.specsNames = append(t.specsNames, name)

	return t.specs[name]
}

func (t *Plugin) CollectSpecs(ctx context.Context) ([]byte, error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("recovered from panic", "plugin", t.name, "context", "CollectSpecs", "error", r)
		}
	}()

	res := make(map[string]any)

	var specErrs error
	for name, spec := range t.specs {
		funcValue := reflect.ValueOf(spec.function)
		input := []reflect.Value{}
		if funcValue.Type().NumIn() == 1 {
			input = append(input, reflect.ValueOf(ctx))
		}
		ret := funcValue.Call(input)

		if len(ret) < 2 {
			return nil, fmt.Errorf("function %s returned insufficient values", name)
		}

		if !ret[0].IsValid() {
			return nil, fmt.Errorf("function %s returned invalid first value", name)
		}

		res[name] = ret[0].Interface()

		if ret[1].IsValid() && ret[1].Interface() != nil {
			err, ok := ret[1].Interface().(error)
			if !ok {
				return nil, errors.New("function returned invalid error")
			}
			if err != nil {
				specErrs = errors.Join(specErrs, err)
			}
		}
	}

	out, err := serializer.JSON.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize spec result: %w", err)
	}

	return out, specErrs
}
