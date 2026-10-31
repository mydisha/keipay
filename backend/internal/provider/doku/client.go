package doku

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// Hosts of DOKU's API per environment.
const (
	sandboxBaseURL    = "https://api-sandbox.doku.com"
	productionBaseURL = "https://api.doku.com"
)

const (
	// maxResponseBody bounds an answer KeiPay reads and stores.
	maxResponseBody = 1 << 20
	// maxErrorBody bounds the part of a failed answer kept in the error.
	maxErrorBody = 1024
)

// answer is DOKU's reply to one request.
type answer struct {
	Status int
	Body   []byte
}

// base is the API host for acc's environment, or the override.
func (a *Adapter) base(acc provider.Account) string {
	if a.baseURL != "" {
		return a.baseURL
	}
	if acc.Environment == domain.EnvLive {
		return productionBaseURL
	}
	return sandboxBaseURL
}

// call sends one signed non-SNAP request. target is the path, which is also
// the signed Request-Target. payload nil sends a GET-style request without a
// body and without a Digest. The error is only about the transport.
func (a *Adapter) call(ctx context.Context, acc provider.Account, method, target string, payload any) (*answer, error) {
	var body []byte
	if payload != nil {
		var err error
		if body, err = json.Marshal(payload); err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base(acc)+target, reader)
	if err != nil {
		return nil, err
	}
	signed{
		ClientID:  acc.Credentials[keyClientID],
		RequestID: newRequestID(),
		Timestamp: timestamp(a.now()),
		Target:    target,
		Body:      body,
	}.setHeaders(req.Header, acc.Credentials[keySecretKey])
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := (&http.Client{Transport: a.transport}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, err
	}
	return &answer{Status: resp.StatusCode, Body: raw}, nil
}

// newRequestID is a unique Request-Id (at most 128 characters).
func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// transportError classifies a request that got no answer. create selects
// the class once the request may have been sent: ErrUnknownOutcome when
// creating, ErrRetryable otherwise.
func transportError(ctx context.Context, create bool, err error) error {
	if !create || (provider.NotSent(err) && ctx.Err() == nil) {
		return fmt.Errorf("%w: %v", provider.ErrRetryable, err)
	}
	return fmt.Errorf("%w: %v", provider.ErrUnknownOutcome, err)
}

// statusError classifies a non-2xx answer: 429 is retryable, 5xx is
// uncertain (ErrUnknownOutcome when creating, else retryable) and any other
// 4xx is a refusal.
func statusError(create bool, ans *answer) error {
	msg := errorMessage(ans.Body)
	switch s := ans.Status; {
	case s == http.StatusTooManyRequests:
		return provider.NewStatusError(provider.ErrRetryable, s, msg)
	case s >= 500 && create:
		return provider.NewStatusError(provider.ErrUnknownOutcome, s, msg)
	case s >= 500:
		return provider.NewStatusError(provider.ErrRetryable, s, msg)
	case s >= 400:
		return provider.NewStatusError(provider.ErrRejected, s, msg)
	case create:
		return provider.NewStatusError(provider.ErrUnknownOutcome, s, "unexpected status")
	default:
		return provider.NewStatusError(provider.ErrRetryable, s, "unexpected status")
	}
}

// errorBody covers the error shapes DOKU answers with.
type errorBody struct {
	ErrorMessages []string `json:"error_messages"`
	Error         *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	ResponseMessage string `json:"responseMessage"`
}

// errorMessage is the readable part of a failed answer, bounded.
func errorMessage(body []byte) string {
	var e errorBody
	msg := ""
	if json.Unmarshal(body, &e) == nil {
		switch {
		case len(e.ErrorMessages) > 0:
			msg = strings.Join(e.ErrorMessages, "; ")
		case e.Error != nil && (e.Error.Message != "" || e.Error.Code != ""):
			msg = strings.TrimSpace(e.Error.Code + " " + e.Error.Message)
		case e.ResponseMessage != "":
			msg = e.ResponseMessage
		}
	}
	if msg == "" {
		msg = string(body)
	}
	if len(msg) > maxErrorBody {
		msg = msg[:maxErrorBody]
	}
	return msg
}
