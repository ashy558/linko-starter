package linkoerr

import "log/slog"

type errWithAttrs struct {
	error
	attrs []slog.Attr
}

func (e *errWithAttrs) Attrs() []slog.Attr {
	return e.attrs
}

func (e *errWithAttrs) Unwrap() error {
	return e.error
}
