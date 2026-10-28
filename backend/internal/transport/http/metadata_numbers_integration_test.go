package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
)

// AC-16 (B6, D-12): metadata numbers are kept as json.Number, but Postgres
// JSONB stores them as numeric. A number numeric cannot hold must be refused
// as invalid_request before anything is written (not a 500 after the Xendit
// invoice exists), and a number Postgres expands on storage must not let the
// stored metadata outgrow the 8192-byte cap that every webhook then carries.

func TestIntegrationMetadataNumberOutsideNumericRangeIsInvalidRequest(t *testing.T) {
	e := newE2E(t)
	for i, n := range []string{"1e200000", "1e-200000"} {
		body := `{"order_id":"meta-range-` + string(rune('a'+i)) + `","amount":10,"metadata":{"n":` + n + `}}`
		w := e.call(http.MethodPost, "/v1/payments", legacyKey, legacySecret, body)
		env := expectError(t, w, http.StatusBadRequest, domain.CodeInvalidRequest)
		if !hasMetadataField(env) {
			t.Fatalf("%s: want a metadata field error, got %+v", n, env.Error.Details)
		}
	}
	if n := e.invoicesCreated(); n != 0 {
		t.Fatalf("invalid metadata opened %d invoices", n)
	}

	_, err := e.appSvc.Create(domain.WithActor(context.Background(), domain.SystemActor("test")), service.CreateAppInput{
		Name: "Numbers", Slug: "numbers", Environment: domain.EnvTest, Metadata: json.RawMessage(`{"n":1e200000}`),
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("app create with an out-of-range number: err = %v, want invalid input", err)
	}
}

func TestIntegrationStoredMetadataStaysWithinByteCap(t *testing.T) {
	e := newE2E(t)
	// 23 bytes as sent; Postgres numeric would store 1e131071 as 131072 digits.
	body := `{"order_id":"meta-size","amount":10,"metadata":{"n":1e131071}}`
	w := e.call(http.MethodPost, "/v1/payments", legacyKey, legacySecret, body)
	if w.Code == http.StatusBadRequest {
		return // refusing the number is fine
	}
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	g := e.call(http.MethodGet, "/v1/payments/"+created.ID, legacyKey, legacySecret, "")
	var got struct {
		Metadata json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal(g.Body.Bytes(), &got); err != nil {
		t.Fatalf("get: %d %v", g.Code, err)
	}
	var compact bytes.Buffer
	_ = json.Compact(&compact, got.Metadata)
	if compact.Len() > 8192 {
		t.Fatalf("stored metadata is %d bytes, over the 8192-byte cap", compact.Len())
	}
}

func TestIntegrationMetadataNumbersReadBackAsCreated(t *testing.T) {
	e := newE2E(t)
	body := `{"order_id":"meta-forms","amount":10,"metadata":{"e":1e3,"u":1E2,"s":1.50e1,"z":-0,"f":-1.0e-3,"big":12345678901234567890}}`
	w := e.call(http.MethodPost, "/v1/payments", legacyKey, legacySecret, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID       string          `json:"id"`
		Metadata json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	g := e.call(http.MethodGet, "/v1/payments/"+created.ID, legacyKey, legacySecret, "")
	var got struct {
		Metadata json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal(g.Body.Bytes(), &got); err != nil {
		t.Fatalf("get: %d %v", g.Code, err)
	}
	const want = `{"big":12345678901234567890,"e":1000,"f":-0.0010,"s":15.0,"u":100,"z":0}`
	for name, raw := range map[string]json.RawMessage{"create": created.Metadata, "get": got.Metadata} {
		if c := compactSorted(t, raw); c != want {
			t.Errorf("%s metadata = %s, want %s", name, c, want)
		}
	}
}

// compactSorted re-encodes a JSON object with sorted keys and numbers kept
// as written.
func compactSorted(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func hasMetadataField(env errorEnvelope) bool {
	raw, _ := json.Marshal(env.Error.Details)
	return bytes.Contains(raw, []byte(`"field":"metadata`))
}
