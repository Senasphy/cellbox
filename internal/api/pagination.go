package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/store"
)

const defaultPageSize = 50
const maxPageSize = 200

type cursorPayload struct {
	Timestamp string `json:"ts"`
	ID        string `json:"id"`
}

func parseListQuery(w http.ResponseWriter, r *http.Request) (int, *store.ListCursor, bool) {
	query := r.URL.Query()
	if len(query["limit"]) > 1 || len(query["cursor"]) > 1 {
		writeListQueryProblem(w, "list_query_invalid", "limit and cursor may appear only once")
		return 0, nil, false
	}
	limit := defaultPageSize
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxPageSize {
			writeListQueryProblem(w, "limit_invalid", fmt.Sprintf("limit must be between 1 and %d", maxPageSize))
			return 0, nil, false
		}
		limit = parsed
	}
	rawCursor := query.Get("cursor")
	if rawCursor == "" {
		return limit, nil, true
	}
	if len(rawCursor) > 512 {
		writeListQueryProblem(w, "cursor_invalid", "cursor is too long")
		return 0, nil, false
	}
	data, err := base64.RawURLEncoding.DecodeString(rawCursor)
	if err != nil {
		writeListQueryProblem(w, "cursor_invalid", "cursor is invalid")
		return 0, nil, false
	}
	var payload cursorPayload
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&payload); err != nil {
		writeListQueryProblem(w, "cursor_invalid", "cursor is invalid")
		return 0, nil, false
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) || payload.ID == "" {
		writeListQueryProblem(w, "cursor_invalid", "cursor is invalid")
		return 0, nil, false
	}
	timestamp, err := time.Parse(time.RFC3339Nano, payload.Timestamp)
	if err != nil || strings.ContainsRune(payload.ID, 0) {
		writeListQueryProblem(w, "cursor_invalid", "cursor is invalid")
		return 0, nil, false
	}
	return limit, &store.ListCursor{Timestamp: timestamp.UTC(), ID: payload.ID}, true
}

func encodeListCursor(timestamp time.Time, id string) string {
	data, _ := json.Marshal(cursorPayload{Timestamp: timestamp.UTC().Format(time.RFC3339Nano), ID: id})
	return base64.RawURLEncoding.EncodeToString(data)
}

func writeListQueryProblem(w http.ResponseWriter, code, detail string) {
	problem.Write(w, problem.Problem{Title: "Invalid list query", Status: http.StatusBadRequest, Code: code, Detail: detail})
}
