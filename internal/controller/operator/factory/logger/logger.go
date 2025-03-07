package logger

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var globalLogger logr.Logger

type contextLoggerKey string

var contextKey contextLoggerKey = "operator_logger_key"

// Logger wraps logger methods with metrics
type Logger struct {
	origin         logr.LogSink
	messageCounter *prometheus.CounterVec
}

// New returns a new logger
// cannot be used concurrently
func New(origin logr.LogSink) logr.Logger {
	messageCounter := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "operator_log_messages_total", Help: "rate of log messages by level"}, []string{"level"})
	registry := metrics.Registry
	registry.MustRegister(messageCounter)
	l := logr.New(&Logger{origin: origin, messageCounter: messageCounter})
	globalLogger = l
	return l
}

// Init implements logr.Logger
func (lw *Logger) Init(info logr.RuntimeInfo) {
	lw.origin.Init(info)
}

// Enabled implements logr.Logger
func (lw *Logger) Enabled(level int) bool {
	return lw.origin.Enabled(level)
}

// Info implements logr.Logger
func (lw *Logger) Info(level int, msg string, keysAndValues ...any) {
	lw.messageCounter.WithLabelValues("info").Inc()
	lw.origin.Info(level, msg, keysAndValues...)
}

// Error implements logr.Logger
func (lw *Logger) Error(err error, msg string, keysAndValues ...any) {
	lw.messageCounter.WithLabelValues("error").Inc()
	lw.origin.Error(err, msg, keysAndValues...)
}

// WithValues implements logr.Logger
func (lw *Logger) WithValues(keysAndValues ...any) logr.LogSink {
	l := *lw
	l.origin = l.origin.WithValues(keysAndValues...)
	return &l
}

// WithName implements logr.Logger
func (lw *Logger) WithName(name string) logr.LogSink {
	l := *lw
	l.origin = l.origin.WithName(name)
	return &l
}

// WithContext returns logger from context or global
func WithContext(ctx context.Context) logr.Logger {
	v, ok := ctx.Value(contextKey).(logr.Logger)
	if ok {
		return v
	}
	return globalLogger
}

// AddToContext adds given logger into context
func AddToContext(ctx context.Context, origin logr.Logger) context.Context {
	return context.WithValue(ctx, contextKey, origin)
}

// SelectedObjects formats and prints into log message selected objects for config reconcile
func SelectedObjects(ctx context.Context, objectName string, selected, broken int, namespacedNames []string) {
	if len(namespacedNames) == 0 {
		return
	}
	formattedNames := "too large to display"
	if len(namespacedNames) < 250 {
		formattedNames = strings.Join(namespacedNames, ",")
	}
	WithContext(ctx).Info(fmt.Sprintf("selected %s count=%d, invalid rules count=%d, namespaced names %s",
		objectName, len(namespacedNames), broken, formattedNames))
}
