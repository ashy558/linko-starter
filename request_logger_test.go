package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func Test_requestLogger(t *testing.T) {
	logBuffer := &bytes.Buffer{}

	logger := slog.New(slog.NewTextHandler(logBuffer, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Time(slog.TimeKey, time.Date(2023, 10, 1, 12, 34, 57, 0, time.UTC))
			}
			if a.Key == "duration" {
				return slog.Duration("duration", time.Duration(0))
			}
			return a
		},
	}))

	requestLoggerMiddleware := requestLogger(logger)
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	loggedHandler := requestLoggerMiddleware(dummyHandler)

	req := httptest.NewRequest("GET", "http://lin.ko/api/stats", nil)
	rr := httptest.NewRecorder()
	loggedHandler.ServeHTTP(rr, req)

	const expectedLogString = `time=2023-10-01T12:34:57.000Z level=INFO msg="Served request" method=GET path=/api/stats request_body_bytes=0 response_status=0 response_body_bytes=0 duration=0s client_ip=192.0.2.x request_id=""` + "\n"
	const expectedStatusCode = http.StatusOK
	actualLogString := logBuffer.String()
	actualStatusCode := rr.Code

	if actualLogString != expectedLogString {
		t.Errorf("Failed request logger test: \nrequest: httptest.NewRequest(\"GET\", \"http://lin.ko/api/stats\", nil) \nexpected: %s, \nactual: %s", expectedLogString, actualLogString)
	}
	if actualStatusCode != expectedStatusCode {
		t.Errorf("Failed request logger test: \nrequest: httptest.NewRequest(\"GET\", \"http://lin.ko/api/stats\", nil) \nexpected: %d, \nactual: %d", expectedStatusCode, actualStatusCode)
	}
}
