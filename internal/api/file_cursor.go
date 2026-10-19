package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type fileCursorPayload struct {
	Path string `json:"path"`
}

func parseFileCursor(w http.ResponseWriter, r *http.Request) (string, bool) {
	values := r.URL.Query()["cursor"]
	if len(values) > 1 {
		writeListQueryProblem(w, "list_query_invalid", "cursor may appear only once")
		return "", false
	}
	if len(values) == 0 || values[0] == "" {
		return "", true
	}
	if len(values[0]) > 4096 {
		writeListQueryProblem(w, "cursor_invalid", "cursor is too long")
		return "", false
	}
	data, err := base64.RawURLEncoding.DecodeString(values[0])
	if err != nil {
		writeListQueryProblem(w, "cursor_invalid", "cursor is invalid")
		return "", false
	}
	var payload fileCursorPayload
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&payload); err != nil {
		writeListQueryProblem(w, "cursor_invalid", "cursor is invalid")
		return "", false
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) || payload.Path == "" || strings.ContainsRune(payload.Path, 0) {
		writeListQueryProblem(w, "cursor_invalid", "cursor is invalid")
		return "", false
	}
	return payload.Path, true
}

func encodeFileCursor(path string) string {
	data, _ := json.Marshal(fileCursorPayload{Path: path})
	return base64.RawURLEncoding.EncodeToString(data)
}
