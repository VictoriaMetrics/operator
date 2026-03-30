package main

import (
	"flag"
	"testing"
)

func TestLogFormatAlias(t *testing.T) {
	f := func(logFormatVal, loggerFormatVal, expected string) {
		t.Helper()
		_ = flag.Set("log-format", logFormatVal)
		_ = flag.Set("loggerFormat", loggerFormatVal)

		if *logFormat != "" {
			_ = flag.Set("loggerFormat", *logFormat)
		}

		loggerFormatFlag := flag.Lookup("loggerFormat")
		if loggerFormatFlag == nil {
			t.Fatalf("expected loggerFormat flag to be registered")
		}

		if loggerFormatFlag.Value.String() != expected {
			t.Fatalf("expected loggerFormat to be %q, got %q", expected, loggerFormatFlag.Value.String())
		}
	}

	// only log-format is set
	f("json", "default", "json")

	// log-format is empty
	f("", "json", "json")
}
