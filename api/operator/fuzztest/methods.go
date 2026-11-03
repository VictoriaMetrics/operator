package fuzztest

import (
	"reflect"
	"strings"

	"sigs.k8s.io/randfill"
)

const unsupportedKindPanic = "BUG unsupported"

// ArgFunc returns a custom value for argument type at, ok=false falls back to fuzzed value.
type ArgFunc func(at reflect.Type) (reflect.Value, bool)

// CallMethods invokes every exported method of v with fuzzed arguments: UnmarshalJSON, DeepCopy etc
func CallMethods(c *randfill.Filler, v any, arg ArgFunc) {
	rv := reflect.ValueOf(v)
	if rv.IsNil() {
		return
	}

	for i := range rv.NumMethod() {
		m := rv.Method(i)
		args, ok := fuzzArgs(c, m.Type(), arg)
		if !ok {
			continue
		}
		callMethod(m, args)
	}
}

// callMethod ignores intentional panics on unsupported cluster components.
func callMethod(m reflect.Value, args []reflect.Value) {
	defer func() {
		if r := recover(); r != nil {
			if s, ok := r.(string); ok && strings.HasPrefix(s, unsupportedKindPanic) {
				return
			}
			panic(r)
		}
	}()
	m.Call(args)
}

// fuzzArgs fills method arguments; skips methods taking interfaces, funcs or chans.
func fuzzArgs(c *randfill.Filler, mt reflect.Type, arg ArgFunc) ([]reflect.Value, bool) {
	args := make([]reflect.Value, mt.NumIn())
	for i := range args {
		at := mt.In(i)
		if arg != nil {
			if v, ok := arg(at); ok {
				args[i] = v
				continue
			}
		}

		switch at.Kind() {
		case reflect.Interface, reflect.Func, reflect.Chan:
			return nil, false
		case reflect.Pointer:
			p := reflect.New(at.Elem())
			c.Fill(p.Interface())
			args[i] = p
		default:
			p := reflect.New(at)
			c.Fill(p.Interface())
			args[i] = p.Elem()
		}
	}
	return args, true
}
