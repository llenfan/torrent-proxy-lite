package logging

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

func TestNewJSONLoggerWritesParsableOutput(t *testing.T) {
	var buf bytes.Buffer
	log, err := New("info", "json", &buf)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Info("hello", "key", "value")
	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log output is not valid JSON: %v\n%s", err, buf.String())
	}
	if entry["msg"] != "hello" {
		t.Errorf("msg = %v, want hello", entry["msg"])
	}
	if entry["key"] != "value" {
		t.Errorf("key = %v, want value", entry["key"])
	}
}

func TestNewFiltersBelowConfiguredLevel(t *testing.T) {
	var buf bytes.Buffer
	log, err := New("info", "text", &buf)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Debug("hidden")
	if buf.Len() != 0 {
		t.Errorf("debug record was written at info level: %s", buf.String())
	}
}

func TestNewRejectsUnknownLevelAndFormat(t *testing.T) {
	if _, err := New("verbose", "json", io.Discard); err == nil {
		t.Error("expected error for unknown level")
	}
	if _, err := New("info", "xml", io.Discard); err == nil {
		t.Error("expected error for unknown format")
	}
}
