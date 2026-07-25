package main

import (
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

var compactBufferPool = buffer.NewPool()

// compactEncoder renders one readable line per entry: short time, level,
// logger, message, then "key=value" pairs in call order — instead of zap's
// stock console encoder, which appends fields as a raw JSON blob and makes
// high-volume RPC logs unreadable at a glance. It writes fields straight
// into a pooled buffer.Buffer (no map/interface{} boxing on the hot path),
// so it stays close to the JSON/console encoders' cost.
type compactEncoder struct {
	cfg zapcore.EncoderConfig
	buf *buffer.Buffer // accumulated fields from Logger.With(...)
}

func newCompactEncoder(cfg zapcore.EncoderConfig) *compactEncoder {
	return &compactEncoder{cfg: cfg, buf: compactBufferPool.Get()}
}

func (e *compactEncoder) Clone() zapcore.Encoder {
	clone := &compactEncoder{cfg: e.cfg, buf: compactBufferPool.Get()}
	clone.buf.AppendString(e.buf.String())
	return clone
}

func (e *compactEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	line := compactBufferPool.Get()

	line.AppendString(ent.Time.Format("15:04:05.000"))
	line.AppendByte(' ')

	lvl := ent.Level.CapitalString()
	line.AppendString(lvl)
	for i := len(lvl); i < 5; i++ {
		line.AppendByte(' ')
	}
	line.AppendByte(' ')

	if ent.LoggerName != "" {
		line.AppendByte('[')
		line.AppendString(ent.LoggerName)
		line.AppendString("] ")
	}
	line.AppendString(ent.Message)

	if e.buf.Len() > 0 {
		line.AppendByte(' ')
		line.AppendString(e.buf.String())
	}

	if len(fields) > 0 {
		tmp := &compactEncoder{cfg: e.cfg, buf: compactBufferPool.Get()}
		for _, f := range fields {
			f.AddTo(tmp)
		}
		if tmp.buf.Len() > 0 {
			line.AppendByte(' ')
			line.AppendString(tmp.buf.String())
		}
		tmp.buf.Free()
	}

	if ent.Caller.Defined {
		line.AppendString("  (")
		line.AppendString(ent.Caller.TrimmedPath())
		line.AppendByte(')')
	}

	if ent.Stack != "" {
		line.AppendByte('\n')
		line.AppendString(ent.Stack)
	}

	line.AppendByte('\n')
	return line, nil
}

func (e *compactEncoder) addKey(key string) {
	if e.buf.Len() > 0 {
		e.buf.AppendByte(' ')
	}
	e.buf.AppendString(key)
	e.buf.AppendByte('=')
}

// appendSafeString keeps "key=value value2" pairs unambiguous: values with
// whitespace, '=' or '"' get quoted, everything else (the vast majority —
// method names, hex ids, enum-ish strings) is written bare.
func (e *compactEncoder) appendSafeString(s string) {
	if s == "" || strings.ContainsAny(s, " \t\n=\"") {
		e.buf.AppendByte('"')
		e.buf.AppendString(strings.ReplaceAll(s, `"`, `\"`))
		e.buf.AppendByte('"')
		return
	}
	e.buf.AppendString(s)
}

func (e *compactEncoder) AddBool(key string, v bool)       { e.addKey(key); e.buf.AppendBool(v) }
func (e *compactEncoder) AddInt64(key string, v int64)     { e.addKey(key); e.buf.AppendInt(v) }
func (e *compactEncoder) AddInt(key string, v int)         { e.AddInt64(key, int64(v)) }
func (e *compactEncoder) AddInt32(key string, v int32)     { e.AddInt64(key, int64(v)) }
func (e *compactEncoder) AddInt16(key string, v int16)     { e.AddInt64(key, int64(v)) }
func (e *compactEncoder) AddInt8(key string, v int8)       { e.AddInt64(key, int64(v)) }
func (e *compactEncoder) AddUint64(key string, v uint64)   { e.addKey(key); e.buf.AppendUint(v) }
func (e *compactEncoder) AddUint(key string, v uint)       { e.AddUint64(key, uint64(v)) }
func (e *compactEncoder) AddUint32(key string, v uint32)   { e.AddUint64(key, uint64(v)) }
func (e *compactEncoder) AddUint16(key string, v uint16)   { e.AddUint64(key, uint64(v)) }
func (e *compactEncoder) AddUint8(key string, v uint8)     { e.AddUint64(key, uint64(v)) }
func (e *compactEncoder) AddUintptr(key string, v uintptr) { e.AddUint64(key, uint64(v)) }
func (e *compactEncoder) AddFloat64(key string, v float64) { e.addKey(key); e.buf.AppendFloat(v, 64) }
func (e *compactEncoder) AddFloat32(key string, v float32) {
	e.addKey(key)
	e.buf.AppendFloat(float64(v), 32)
}
func (e *compactEncoder) AddDuration(key string, v time.Duration) {
	e.addKey(key)
	e.buf.AppendString(v.String())
}
func (e *compactEncoder) AddTime(key string, v time.Time) {
	e.addKey(key)
	e.buf.AppendTime(v, "15:04:05.000")
}
func (e *compactEncoder) AddString(key, v string) { e.addKey(key); e.appendSafeString(v) }
func (e *compactEncoder) AddByteString(key string, v []byte) {
	e.addKey(key)
	e.appendSafeString(string(v))
}
func (e *compactEncoder) AddBinary(key string, v []byte) { e.addKey(key); fmt.Fprintf(e.buf, "%x", v) }
func (e *compactEncoder) AddComplex128(key string, v complex128) {
	e.addKey(key)
	fmt.Fprintf(e.buf, "%v", v)
}
func (e *compactEncoder) AddComplex64(key string, v complex64) { e.AddComplex128(key, complex128(v)) }
func (e *compactEncoder) AddReflected(key string, v interface{}) error {
	e.addKey(key)
	fmt.Fprintf(e.buf, "%v", v)
	return nil
}

// OpenNamespace isn't used by any log call in this codebase today; nested
// fields would just render as flat top-level keys, which is an acceptable
// degradation for a format whose whole point is staying flat and scannable.
func (e *compactEncoder) OpenNamespace(string) {}

func (e *compactEncoder) AddArray(key string, m zapcore.ArrayMarshaler) error {
	tmp := zapcore.NewMapObjectEncoder()
	if err := tmp.AddArray("v", m); err != nil {
		e.addKey(key)
		e.buf.AppendString("!ERROR")
		return err
	}
	e.addKey(key)
	fmt.Fprintf(e.buf, "%v", tmp.Fields["v"])
	return nil
}

func (e *compactEncoder) AddObject(key string, m zapcore.ObjectMarshaler) error {
	tmp := zapcore.NewMapObjectEncoder()
	if err := tmp.AddObject("v", m); err != nil {
		e.addKey(key)
		e.buf.AppendString("!ERROR")
		return err
	}
	e.addKey(key)
	fmt.Fprintf(e.buf, "%v", tmp.Fields["v"])
	return nil
}
