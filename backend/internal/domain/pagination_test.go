package domain

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestCursorRoundTrip(t *testing.T) {
	cases := []Cursor{
		{Time: time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC), ID: "aud_01JBKQ7Z8M4X2Y6V3N9T5R1W0C"},
		{Time: time.Date(2026, 1, 2, 10, 4, 5, 1000, time.FixedZone("WIB", 7*3600)), ID: "x"},
	}
	for _, c := range cases {
		got, err := DecodeCursor(EncodeCursor(c))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !got.Time.Equal(c.Time) || got.ID != c.ID || got.Time.Location() != time.UTC {
			t.Fatalf("round trip = %+v, want %+v in UTC", got, c)
		}
	}
}

func TestCursorIsURLSafe(t *testing.T) {
	s := EncodeCursor(Cursor{Time: time.Now(), ID: "aud_01JBKQ7Z8M4X2Y6V3N9T5R1W0C"})
	for _, r := range s {
		if r == '+' || r == '/' || r == '=' {
			t.Fatalf("cursor %q is not unpadded base64url", s)
		}
	}
}

func TestDecodeCursorRejectsMalformed(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	cases := map[string]string{
		"bad base64":   "%%%not-base64",
		"padded":       base64.URLEncoding.EncodeToString([]byte(`{"v":1,"t":"2026-01-02T03:04:05Z","id":"a"}`)) + "==",
		"bad json":     enc(`{"v":1,`),
		"version 2":    enc(`{"v":2,"t":"2026-01-02T03:04:05Z","id":"a"}`),
		"no version":   enc(`{"t":"2026-01-02T03:04:05Z","id":"a"}`),
		"zero time":    enc(`{"v":1,"t":"0001-01-01T00:00:00Z","id":"a"}`),
		"bad time":     enc(`{"v":1,"t":"yesterday","id":"a"}`),
		"empty id":     enc(`{"v":1,"t":"2026-01-02T03:04:05Z","id":""}`),
		"json array":   enc(`[1,2]`),
		"empty string": "",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeCursor(in)
			if !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("err = %v, want ErrInvalidCursor", err)
			}
			var coded *Error
			if !errors.As(err, &coded) || coded.Code != CodeInvalidCursor {
				t.Fatalf("err = %v, want code invalid_cursor", err)
			}
		})
	}
}

func TestPageRequestPage(t *testing.T) {
	p, err := PageRequest{}.Page()
	if err != nil || p.Limit != DefaultPageLimit || p.After != nil {
		t.Fatalf("default page = %+v, %v", p, err)
	}
	for _, limit := range []int{1, MaxPageLimit} {
		if p, err := (PageRequest{Limit: limit}).Page(); err != nil || p.Limit != limit {
			t.Fatalf("limit %d: %+v, %v", limit, p, err)
		}
	}
	for _, limit := range []int{-1, MaxPageLimit + 1} {
		if _, err := (PageRequest{Limit: limit}).Page(); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("limit %d: err = %v, want ErrInvalidInput", limit, err)
		}
	}
	if _, err := (PageRequest{Cursor: "garbage"}).Page(); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("bad cursor: err = %v, want ErrInvalidCursor", err)
	}
}
