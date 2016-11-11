package main

import (
	"log"
	"os"
)

// Logger represents a logging object.
type Logger struct {
	debug bool
	*log.Logger
}

// NewLogger creates a new Logger. The debug variable sets the Debugf/Debugln
// methods enablement.
func NewLogger(prefix string, debug bool) *Logger {
	return &Logger{
		debug:  debug,
		Logger: log.New(os.Stdout, prefix, 0),
	}
}

// Debugf acts just like fmt.Printf.
func (l *Logger) Debugf(format string, v ...interface{}) {
	if l.debug {
		l.Printf(format, v...)
	}
}

// Debugln acts just like fmt.Println.
func (l *Logger) Debugln(v ...interface{}) {
	if l.debug {
		l.Println(v...)
	}
}
