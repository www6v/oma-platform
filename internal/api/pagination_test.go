package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestWriteListPageNilSliceEmitsEmptyArray locks in the fix for a recurring
// footgun: Go's encoding/json marshals a nil slice as `null`, but the
// console frontend (and API consumers) expect `[]` for empty lists. When
// a tenant has no environments/agents/sessions yet, the repo returns a
// nil []*T; writeListPage must coerce that to `[]` before encoding.
func TestWriteListPageNilSliceEmitsEmptyArray(t *testing.T) {
	type env struct {
		ID string `json:"id"`
	}

	cases := []struct {
		name string
		data any
		want string
	}{
		{"nil typed slice", ([]*env)(nil), `{"data":[],"has_more":false}`},
		{"empty non-nil slice", []*env{}, `{"data":[],"has_more":false}`},
		{"populated slice", []*env{{ID: "env-1"}}, `{"data":[{"id":"env-1"}],"has_more":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeListPage(rec, tc.data, "")
			body := rec.Body.String()
			// Trim trailing newline from json.Encoder.
			if n := len(body); n > 0 && body[n-1] == '\n' {
				body = body[:n-1]
			}
			if body != tc.want {
				t.Fatalf("body = %q, want %q", body, tc.want)
			}
			// Sanity: round-trip parses as JSON with data as array.
			var parsed map[string]any
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if _, ok := parsed["data"].([]any); !ok {
				t.Fatalf("data field is not an array: %#v", parsed["data"])
			}
		})
	}
}
