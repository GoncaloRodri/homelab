package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	red    = "\033[31m"
	yellow = "\033[33m"
	blue   = "\033[34m"
	cyan   = "\033[36m"
	gray   = "\033[90m"
	reset  = "\033[0m"
)

func Init() {
	var lvl slog.Level
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	var h slog.Handler
	if isTerminal() {
		h = &colorHandler{level: lvl}
	} else {
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	}

	slog.SetDefault(slog.New(h))
}

func isTerminal() bool {
	o, _ := os.Stdout.Stat()
	return (o.Mode() & os.ModeCharDevice) != 0
}

type colorHandler struct {
	level slog.Level
}

func (h *colorHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *colorHandler) Handle(_ context.Context, r slog.Record) error {
	buf := make([]byte, 0, 256)

	timebuf := make([]byte, 0, 16)
	timebuf = r.Time.AppendFormat(timebuf, time.TimeOnly)
	buf = appendTo(buf, gray)
	buf = append(buf, timebuf...)
	buf = append(buf, " | "...)
	buf = appendTo(buf, reset)

	buf = appendTo(buf, levelColor(r.Level))
	buf = append(buf, levelPad(r.Level)...)
	buf = append(buf, " | "...)
	buf = appendTo(buf, reset)

	buf = append(buf, r.Message...)

	r.Attrs(func(a slog.Attr) bool {
		buf = appendTo(buf, gray)
		buf = append(buf, "  "...)
		buf = append(buf, a.Key...)
		buf = append(buf, "="...)
		buf = appendTo(buf, reset)
		buf = append(buf, fmtAttr(a.Value)...)
		return true
	})

	buf = append(buf, '\n')
	os.Stdout.Write(buf)
	return nil
}

func (h *colorHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *colorHandler) WithGroup(_ string) slog.Handler {
	return h
}

func levelColor(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return red
	case l >= slog.LevelWarn:
		return yellow
	case l >= slog.LevelInfo:
		return cyan
	default:
		return blue
	}
}

func levelPad(l slog.Level) string {
	s := strings.ToUpper(l.String())
	if len(s) < 5 {
		s += strings.Repeat(" ", 5-len(s))
	}
	return s
}

func fmtAttr(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return strconv.FormatInt(v.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(v.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(v.Float64(), 'g', -1, 64)
	case slog.KindBool:
		return strconv.FormatBool(v.Bool())
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format(time.RFC3339)
	default:
		return fmt.Sprintf("%+v", v.Any())
	}
}

func appendTo(buf []byte, s string) []byte {
	return append(buf, []byte(s)...)
}
