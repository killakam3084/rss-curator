package logbuffer

import (
	"fmt"

	"go.uber.org/zap/zapcore"
)

// bufferCore is a zapcore.Core that writes structured log entries into a
// Buffer so they can be served over the /api/logs SSE stream.
type bufferCore struct {
	buf    *Buffer
	level  zapcore.LevelEnabler
	fields []zapcore.Field
}

// NewZapCore returns a zapcore.Core that feeds every log entry into buf.
// Compose it with the normal production core via zapcore.NewTee so that
// logs go to both stdout and the in-memory buffer.
func NewZapCore(buf *Buffer) zapcore.Core {
	return &bufferCore{
		buf:   buf,
		level: zapcore.DebugLevel,
	}
}

func (c *bufferCore) Enabled(level zapcore.Level) bool {
	return c.level.Enabled(level)
}

func (c *bufferCore) With(fields []zapcore.Field) zapcore.Core {
	clone := *c
	clone.fields = append(append([]zapcore.Field{}, c.fields...), fields...)
	return &clone
}

func (c *bufferCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

func (c *bufferCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	all := append(append([]zapcore.Field{}, c.fields...), fields...)
	var fm map[string]any
	if len(all) > 0 {
		fm = make(map[string]any, len(all))
		for _, f := range all {
			fm[f.Key] = fieldValue(f)
		}
	}
	c.buf.Append(entry.Level.CapitalString(), entry.Message, fm)
	return nil
}

func (c *bufferCore) Sync() error { return nil }

// fieldValue extracts a printable value from a zapcore.Field.
func fieldValue(f zapcore.Field) any {
	switch f.Type {
	case zapcore.StringType:
		return f.String
	case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
		return f.Integer
	case zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
		return uint64(f.Integer)
	case zapcore.BoolType:
		return f.Integer == 1
	case zapcore.Float64Type, zapcore.Float32Type:
		return f.Integer
	case zapcore.ErrorType:
		if f.Interface != nil {
			return fmt.Sprintf("%v", f.Interface)
		}
		return nil
	case zapcore.SkipType:
		return nil
	default:
		if f.Interface != nil {
			return fmt.Sprintf("%v", f.Interface)
		}
		return f.String
	}
}
