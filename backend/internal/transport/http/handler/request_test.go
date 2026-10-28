package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
)

func testContext(method, target, body string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	return c
}

func fieldsOf(t *testing.T, err error) []domain.FieldError {
	t.Helper()
	var coded *domain.Error
	if !errors.As(err, &coded) || coded.Code != domain.CodeInvalidRequest {
		t.Fatalf("err = %v, want invalid_request", err)
	}
	fields, ok := coded.Details["fields"].([]domain.FieldError)
	if !ok {
		t.Fatalf("details.fields = %#v, want a list", coded.Details["fields"])
	}
	if strings.Contains(coded.Message, "CreatePaymentRequest") {
		t.Fatalf("message leaks a Go type name: %q", coded.Message)
	}
	return fields
}

func TestBindJSONReportsJSONFieldNames(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []domain.FieldError
	}{
		{"missing order_id", `{"amount":1000}`, []domain.FieldError{{Field: "order_id", Rule: "required"}}},
		{"amount wrong type", `{"order_id":"o1","amount":"x"}`, []domain.FieldError{{Field: "amount", Rule: "type"}}},
		{"item quantity zero", `{"order_id":"o1","amount":1000,"items":[{"name":"Tea","quantity":0,"price":1000}]}`,
			[]domain.FieldError{{Field: "items.0.quantity", Rule: "required"}}},
		{"second item missing name", `{"order_id":"o1","amount":1000,"items":[{"name":"Tea","quantity":1},{"quantity":1}]}`,
			[]domain.FieldError{{Field: "items.1.name", Rule: "required"}}},
		{"several fields", `{}`, []domain.FieldError{{Field: "order_id", Rule: "required"}, {Field: "amount", Rule: "required"}}},
		{"malformed json", `{"order_id":`, []domain.FieldError{}},
		{"empty body", ``, []domain.FieldError{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req dto.CreatePaymentRequest
			err := bindJSON(testContext(http.MethodPost, "/v1/payments", tc.body), &req)
			if got := fieldsOf(t, err); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("fields = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestBindJSONNestedTypeError(t *testing.T) {
	var req dto.CreatePaymentRequest
	err := bindJSON(testContext(http.MethodPost, "/v1/payments", `{"order_id":"o1","amount":1,"items":[{"name":"a","quantity":"x"}]}`), &req)
	want := []domain.FieldError{{Field: "items.0.quantity", Rule: "type"}}
	if got := fieldsOf(t, err); !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %+v, want %+v", got, want)
	}
}

func TestBindJSONMaxRule(t *testing.T) {
	var req dto.LoginRequest
	body := `{"email":"a@example.com","password":"` + strings.Repeat("x", 1025) + `"}`
	err := bindJSON(testContext(http.MethodPost, "/admin/login", body), &req)
	want := []domain.FieldError{{Field: "password", Rule: "max"}}
	if got := fieldsOf(t, err); !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %+v, want %+v", got, want)
	}
}

func TestBindJSONAcceptsValidBody(t *testing.T) {
	var req dto.CreatePaymentRequest
	body := `{"order_id":"ord-ü","amount":150000,"items":[{"name":"Kopi","quantity":2,"price":75000}]}`
	if err := bindJSON(testContext(http.MethodPost, "/v1/payments", body), &req); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if req.OrderID != "ord-ü" || req.Amount != 150000 || len(req.Items) != 1 {
		t.Fatalf("req = %+v", req)
	}
}

func TestJSONPath(t *testing.T) {
	for in, want := range map[string]string{
		"CreatePaymentRequest.order_id":          "order_id",
		"CreatePaymentRequest.items[0].quantity": "items.0.quantity",
		"CreatePaymentRequest.customer.email":    "customer.email",
		"CreatePaymentRequest.items[12].name":    "items.12.name",
		"order_id":                               "order_id",
	} {
		if got := jsonPath(in); got != want {
			t.Errorf("jsonPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPathID(t *testing.T) {
	c := testContext(http.MethodGet, "/x", "")
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	_, err := pathID(c)
	want := []domain.FieldError{{Field: "id", Rule: "int"}}
	if got := fieldsOf(t, err); !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %+v, want %+v", got, want)
	}
	c.Params = gin.Params{{Key: "id", Value: "42"}}
	if id, err := pathID(c); err != nil || id != 42 {
		t.Fatalf("pathID = %d, %v", id, err)
	}
}

func TestPageRequestLimit(t *testing.T) {
	for query, want := range map[string]int{"": 50, "?limit=": 50, "?limit=1": 1, "?limit=200": 200} {
		req, err := pageRequest(testContext(http.MethodGet, "/admin/audit-logs"+query, ""))
		if err != nil || req.Limit != want {
			t.Fatalf("%q: limit = %d, %v; want %d", query, req.Limit, err, want)
		}
	}
	for _, v := range []string{"0", "201", "-1", "abc", "1.5", "99999999999999999999"} {
		_, err := pageRequest(testContext(http.MethodGet, "/admin/audit-logs?limit="+v, ""))
		want := []domain.FieldError{{Field: "limit", Rule: "range"}}
		if got := fieldsOf(t, err); !reflect.DeepEqual(got, want) {
			t.Fatalf("limit=%s: fields = %+v, want %+v", v, got, want)
		}
	}
}

func TestPageRequestCursor(t *testing.T) {
	cur := domain.EncodeCursor(domain.Cursor{Time: time.Now(), ID: "aud_01JBKQ7Z8M4X2Y6V3N9T5R1W0C"})
	req, err := pageRequest(testContext(http.MethodGet, "/admin/audit-logs?cursor="+cur, ""))
	if err != nil || req.Cursor != cur {
		t.Fatalf("cursor = %q, %v", req.Cursor, err)
	}
	if _, err := pageRequest(testContext(http.MethodGet, "/admin/audit-logs?cursor=bogus!", "")); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("bad cursor err = %v, want ErrInvalidCursor", err)
	}
}
