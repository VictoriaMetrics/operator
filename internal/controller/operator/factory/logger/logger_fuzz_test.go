package logger

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	"github.com/prometheus/client_golang/prometheus"
)

func FuzzLogger(f *testing.F) {
	f.Add("vmrule", 1, 0, "a/b,c/d", "msg", "k", false)
	f.Add("", 0, 0, "", "", "", true)
	f.Add("x", -1, 300, strings.Repeat("n/n,", 300), "m", "", true)
	f.Fuzz(func(t *testing.T, name string, selected, broken int, names, msg, key string, withCtx bool) {
		counter := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "fuzz"}, []string{"level"})
		sink := funcr.New(func(_, _ string) {}, funcr.Options{Verbosity: 1})
		l := logr.New(&Logger{origin: sink.GetSink(), messageCounter: counter}).WithName(name).WithValues(key, msg)

		// global logger is used without context logger
		ctx := context.Background()
		if withCtx {
			ctx = AddToContext(ctx, l)
		} else {
			prev := globalLogger
			globalLogger = l
			t.Cleanup(func() { globalLogger = prev })
		}
		WithContext(ctx).Info(msg, key, name)
		WithContext(ctx).Error(errors.New(msg), msg, key)
		SelectedObjects(ctx, name, selected, broken, strings.Split(names, ","))
	})
}
