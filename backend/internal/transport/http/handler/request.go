package handler

import (
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
)

// validate checks the `binding` tags on request DTOs and reports fields by
// their JSON names. It is built once and only read afterwards.
var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New()
	v.SetTagName("binding")
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		if name == "" {
			return f.Name
		}
		return name
	})
	return v
}

// bindJSON decodes the request body into dst and validates it. Failures are
// invalid_request errors listing the offending fields.
func bindJSON(c *gin.Context, dst any) error {
	if c.Request.Body == nil {
		return invalidBody()
	}
	if err := json.NewDecoder(c.Request.Body).Decode(dst); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Field != "" {
			return invalidBody(domain.FieldError{Field: typeErr.Field, Rule: "type"})
		}
		return invalidBody()
	}
	if err := validate.Struct(dst); err != nil {
		var verrs validator.ValidationErrors
		if !errors.As(err, &verrs) {
			return err
		}
		fields := make([]domain.FieldError, 0, len(verrs))
		for _, fe := range verrs {
			fields = append(fields, domain.FieldError{Field: jsonPath(fe.Namespace()), Rule: fe.Tag()})
		}
		return invalidBody(fields...)
	}
	return nil
}

var indexRe = regexp.MustCompile(`\[([^\]]*)\]`)

// jsonPath turns a validator namespace such as "CreatePaymentRequest.items[0].quantity"
// into the dot path "items.0.quantity".
func jsonPath(namespace string) string {
	_, rest, ok := strings.Cut(namespace, ".")
	if !ok {
		rest = namespace
	}
	return indexRe.ReplaceAllString(rest, ".$1")
}

func invalidBody(fields ...domain.FieldError) *domain.Error {
	return invalidRequest("invalid request body", fields...)
}

// invalidRequest builds an invalid_request error. details.fields is always a
// list, empty when no single field is at fault.
func invalidRequest(message string, fields ...domain.FieldError) *domain.Error {
	if fields == nil {
		fields = []domain.FieldError{}
	}
	return &domain.Error{
		Code:    domain.CodeInvalidRequest,
		Message: message,
		Details: map[string]any{"fields": fields},
	}
}

// pathID parses the :id path parameter.
func pathID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return 0, invalidRequest("invalid path parameter", domain.FieldError{Field: "id", Rule: "int"})
	}
	return id, nil
}

// pathUserID parses the :id path parameter as a usr_ id (lower case
// accepted) and returns its canonical form.
func pathUserID(c *gin.Context) (string, error) {
	uid, err := id.ParseWithPrefix(c.Param("id"), id.User)
	if err != nil {
		return "", invalidRequest("invalid path parameter", domain.FieldError{Field: "id", Rule: "public_id"})
	}
	return uid, nil
}

// clientMeta describes the caller for sessions, rate limits and audit.
func clientMeta(c *gin.Context) service.ClientMeta {
	return service.ClientMeta{
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		RequestID: middleware.RequestIDFromContext(c),
	}
}

// pageRequest parses the limit and cursor query parameters of a
// cursor-paginated list.
func pageRequest(c *gin.Context) (domain.PageRequest, error) {
	req := domain.PageRequest{Limit: domain.DefaultPageLimit, Cursor: c.Query("cursor")}
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > domain.MaxPageLimit {
			return domain.PageRequest{}, invalidRequest("invalid query parameter", domain.FieldError{Field: "limit", Rule: "range"})
		}
		req.Limit = n
	}
	if req.Cursor != "" {
		if _, err := domain.DecodeCursor(req.Cursor); err != nil {
			return domain.PageRequest{}, err
		}
	}
	return req, nil
}
