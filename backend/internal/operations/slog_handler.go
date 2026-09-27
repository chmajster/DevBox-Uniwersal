package operations

import (
	"context"
	"log/slog"
	"strings"
)

type SlogCaptureHandler struct {
	next   slog.Handler
	source *RingLogSource
	attrs  []slog.Attr
	groups []string
}

func NewSlogCaptureHandler(next slog.Handler, source *RingLogSource) *SlogCaptureHandler {
	return &SlogCaptureHandler{next: next, source: source}
}

func (h *SlogCaptureHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *SlogCaptureHandler) Handle(ctx context.Context, record slog.Record) error {
	fields := make(map[string]any, len(h.attrs)+record.NumAttrs())
	for _, attr := range h.attrs {
		fields[h.attributeKey(attr.Key)] = attr.Value.Any()
	}
	record.Attrs(func(attr slog.Attr) bool {
		fields[h.attributeKey(attr.Key)] = attr.Value.Any()
		return true
	})
	h.source.Append(strings.ToLower(record.Level.String()), record.Message, fields)
	return h.next.Handle(ctx, record)
}

func (h *SlogCaptureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	combined = append(combined, h.attrs...)
	combined = append(combined, attrs...)
	return &SlogCaptureHandler{
		next:   h.next.WithAttrs(attrs),
		source: h.source,
		attrs:  combined,
		groups: append([]string(nil), h.groups...),
	}
}

func (h *SlogCaptureHandler) WithGroup(name string) slog.Handler {
	groups := append([]string(nil), h.groups...)
	if name != "" {
		groups = append(groups, name)
	}
	return &SlogCaptureHandler{
		next:   h.next.WithGroup(name),
		source: h.source,
		attrs:  append([]slog.Attr(nil), h.attrs...),
		groups: groups,
	}
}

func (h *SlogCaptureHandler) attributeKey(key string) string {
	if len(h.groups) == 0 {
		return key
	}
	return strings.Join(append(append([]string(nil), h.groups...), key), ".")
}
