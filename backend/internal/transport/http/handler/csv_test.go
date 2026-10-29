package handler

import (
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Text cells that a spreadsheet would run as a formula get a leading quote;
// everything else is left alone.
func TestCSVText(t *testing.T) {
	cases := map[string]string{
		"=cmd":      "'=cmd",
		"+1":        "'+1",
		"-x":        "'-x",
		"@a":        "'@a",
		"\tx":       "'\tx",
		"\rx":       "'\rx",
		"":          "",
		"kp_pay_A":  "kp_pay_A",
		"a=b":       "a=b",
		"ü-order":   "ü-order",
		" =padded":  " =padded",
		"'=already": "'=already",
	}
	for in, want := range cases {
		if got := csvText(in); got != want {
			t.Errorf("csvText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCSVCells(t *testing.T) {
	at := time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC)
	if got := csvTime(&at); got != "2026-11-01T00:00:00+07:00" {
		t.Fatalf("csvTime = %q", got)
	}
	if csvTime(nil) != "" || csvInt(nil) != "" || csvDate(nil) != "all" {
		t.Fatal("empty cells")
	}
	n := int64(-5)
	if csvInt(&n) != "-5" {
		t.Fatalf("csvInt = %q, amounts are never guarded", csvInt(&n))
	}
	if got := csvDate(&at); got != "20261101" {
		t.Fatalf("csvDate = %q", got)
	}
}

// csvServer serves one export through streamCSV on a real server with a
// short write timeout, so the deadline handling is what net/http applies.
func csvServer(t *testing.T, writeTimeout time.Duration, produce func(write func([]string) error) error) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/export", func(c *gin.Context) {
		streamCSV(c, zap.NewNop(), "rows.csv", []string{"n"}, produce)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: r, WriteTimeout: writeTimeout, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String() + "/export"
}

func rows(n int) [][]string {
	out := make([][]string, n)
	for i := range out {
		out[i] = []string{strconv.Itoa(i)}
	}
	return out
}

// An export that outlives the server's write timeout still arrives whole:
// every flush moves the write deadline forward.
func TestStreamCSVOutlivesWriteTimeout(t *testing.T) {
	url := csvServer(t, 250*time.Millisecond, func(write func([]string) error) error {
		for _, row := range rows(csvFlushEvery) {
			if err := write(row); err != nil {
				return err
			}
		}
		time.Sleep(400 * time.Millisecond)
		for _, row := range rows(csvFlushEvery) {
			if err := write(row); err != nil {
				return err
			}
		}
		return nil
	})
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if lines := strings.Count(string(body), "\n"); lines != 2*csvFlushEvery+1 {
		t.Fatalf("got %d lines, want header plus %d rows", lines, 2*csvFlushEvery)
	}
}

// The grace is per flush, not unlimited: when the client stops reading,
// the next flush fails within the grace instead of blocking the handler for
// as long as the connection lives.
func TestStreamCSVStalledClientIsCutOff(t *testing.T) {
	grace := csvWriteGrace
	csvWriteGrace = 300 * time.Millisecond
	t.Cleanup(func() { csvWriteGrace = grace })
	failed := make(chan error, 1)
	wide := []string{strings.Repeat("x", 64<<10)}
	url := csvServer(t, time.Hour, func(write func([]string) error) error {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if err := write(wide); err != nil {
				failed <- err
				return err
			}
		}
		failed <- nil
		return nil
	})
	// A raw client that sends the request and never reads the response, so
	// the server's writes block once the socket buffers are full.
	conn, err := net.Dial("tcp", strings.TrimPrefix(url, "http://")[:strings.Index(strings.TrimPrefix(url, "http://"), "/")])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("GET /export HTTP/1.1\r\nHost: test\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-failed:
		if err == nil {
			t.Fatal("the producer finished although nobody read the export")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stalled export was not cut off within the grace")
	}
}
