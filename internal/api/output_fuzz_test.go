package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/store"
)

func FuzzOutputAndPagination(f *testing.F) {
	f.Add("10", "cursor", "stdout", []byte("hello world"), int64(0), 64)
	f.Add("200", encodeListCursor(time.Unix(1700000000, 0).UTC(), "ses_1"), "stderr", []byte{0xff, 0x00, 0x41}, int64(5), 2)
	f.Add("-1", "not-base64!!!", "stdout", []byte("abc"), int64(1), 0)

	f.Fuzz(func(t *testing.T, limitRaw, cursorRaw, stream string, chunk []byte, offset int64, capBytes int) {
		query := url.Values{}
		if limitRaw != "" {
			query.Set("limit", limitRaw)
		}
		if cursorRaw != "" {
			query.Set("cursor", cursorRaw)
		}
		req := httptest.NewRequest(http.MethodGet, "/v1/sessions?"+query.Encode(), nil)
		rec := httptest.NewRecorder()
		limit, cursor, ok := parseListQuery(rec, req)
		if ok {
			if limit < 1 || limit > maxPageSize {
				t.Fatalf("parseListQuery accepted invalid limit %d", limit)
			}
			if cursor != nil && cursor.ID == "" {
				t.Fatalf("parseListQuery returned empty cursor ID")
			}
		}

		items := []store.ExecOutputRecord{
			{Offset: offset, Timestamp: time.Unix(1700000000, 0).UTC(), Stream: stream, Data: chunk},
		}
		_ = encodeOutput(items, capBytes)
	})
}
