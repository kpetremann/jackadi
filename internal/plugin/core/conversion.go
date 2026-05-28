package core

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/kpetremann/jackadi/internal/serializer"
	"github.com/spf13/cast"
)

func StructpbValueToInput(value any, targetType reflect.Type) (any, error) {
	val := reflect.ValueOf(value)

	// Handle pointers
	if targetType.Kind() == reflect.Pointer {
		elemType := targetType.Elem()
		converted, err := StructpbValueToInput(value, elemType)
		if err != nil {
			return nil, err
		}
		ptr := reflect.New(elemType)
		ptr.Elem().Set(reflect.ValueOf(converted))
		return ptr.Interface(), nil
	}

	//nolint:exhaustive // we do not support all types
	switch targetType.Kind() {
	case reflect.Int:
		return cast.ToIntE(value)
	case reflect.Int8:
		return cast.ToInt8E(value)
	case reflect.Int16:
		return cast.ToInt16E(value)
	case reflect.Int32:
		return cast.ToInt32E(value)
	case reflect.Int64:
		return cast.ToInt64E(value)
	case reflect.Uint:
		return cast.ToUintE(value)
	case reflect.Uint8:
		return cast.ToUint8E(value)
	case reflect.Uint16:
		return cast.ToUint16E(value)
	case reflect.Uint32:
		return cast.ToUint32E(value)
	case reflect.Uint64:
		return cast.ToUint64E(value)
	case reflect.Float32:
		return cast.ToFloat32E(value)
	case reflect.Float64:
		return cast.ToFloat64E(value)
	case reflect.Bool:
		return cast.ToBoolE(value)
	case reflect.String:
		return cast.ToStringE(value)
	case reflect.Struct, reflect.Slice, reflect.Array, reflect.Map:
		// first mashalled to JSON then unquoted
		// then unmarshal to the expected struct in the plugin definition
		out := reflect.New(targetType).Interface() // we cannot do Elem() here because it would create a map[string]any
		js, err := serializer.JSON.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal argument %v to %v", value, targetType)
		}

		unquoted, err := strconv.Unquote(string(js)) // TODO: is it needed???
		if err != nil {
			return nil, fmt.Errorf("unable to unquote argument %v", value)
		}

		if err := serializer.JSON.Unmarshal([]byte(unquoted), &out); err != nil {
			return nil, fmt.Errorf("unable to unmarshal argument %v to %v", value, targetType)
		}

		// reflect.New created a pointer. Elem() enables us to have the value instead as expected.
		return reflect.ValueOf(out).Elem().Interface(), nil

	case reflect.Interface:
		// For interfaces, we just return the value as is, if it's assignable.
		if val.Type().AssignableTo(targetType) {
			return value, nil
		}
		return nil, fmt.Errorf("value %v is not assignable to interface %v", value, targetType)
	}

	return nil, fmt.Errorf("unsupported target type: %v", targetType)
}
