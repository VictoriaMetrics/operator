package logger

import (
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

type Logger struct {
	origin         logr.LogSink
	messageCounter *prometheus.CounterVec
}

func New(origin logr.LogSink) logr.Logger {
	messageCounter := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "operator_log_messages_total", Help: "rate of log messages by level"}, []string{"level"})
	registry := metrics.Registry
	registry.MustRegister(messageCounter)
	return logr.New(&Logger{origin: origin, messageCounter: messageCounter})
}

func (lw *Logger) Init(info logr.RuntimeInfo) {
	lw.origin.Init(info)
}

func (lw *Logger) Enabled(level int) bool {
	return lw.origin.Enabled(level)
}

func (lw *Logger) Info(level int, msg string, keysAndValues ...interface{}) {
	lw.messageCounter.WithLabelValues("info").Inc()
	lw.origin.Info(level, msg, keysAndValues...)
}

func (lw *Logger) Error(err error, msg string, keysAndValues ...interface{}) {
	lw.messageCounter.WithLabelValues("error").Inc()
	lw.origin.Error(err, msg, keysAndValues...)
}

func (lw *Logger) WithValues(keysAndValues ...interface{}) logr.LogSink {
	l := *lw
	l.origin = l.origin.WithValues(keysAndValues...)
	return &l
}

func (lw *Logger) WithName(name string) logr.LogSink {
	l := *lw
	l.origin = l.origin.WithName(name)
	return &l
}
