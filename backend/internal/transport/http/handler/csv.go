package handler

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/pkg/logger"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// csvFlushEvery is how many rows are buffered before they are pushed to the
// client.
const csvFlushEvery = 500

// wib is Asia/Jakarta. Indonesia has no daylight saving time, so a fixed
// zone is exact and needs no tzdata in the image.
var wib = time.FixedZone("WIB", 7*60*60)

// csvStream writes rows as a CSV attachment. Rows are buffered until the
// first flush, so a failure until then still gets a JSON error.
type csvStream struct {
	c        *gin.Context
	log      *zap.Logger
	filename string
	header   []string
	w        *csv.Writer
	rows     int
}

// streamCSV calls produce, which writes the rows through its argument. An
// error before the first byte reaches the client is answered as JSON; after
// it the response is cut off, so the client sees an incomplete download
// rather than a short file that looks complete.
func streamCSV(c *gin.Context, log *zap.Logger, filename string, header []string, produce func(write func([]string) error) error) {
	s := &csvStream{c: c, log: log, filename: filename, header: header}
	err := produce(s.write)
	if err == nil {
		err = s.finish()
	}
	if err == nil {
		return
	}
	if !c.Writer.Written() {
		for _, h := range []string{"Content-Type", "Content-Disposition", "Cache-Control", "X-Content-Type-Options"} {
			c.Writer.Header().Del(h)
		}
		response.Error(c, err)
		return
	}
	logger.Ctx(c.Request.Context(), log).Error("csv export aborted",
		zap.String(logger.FieldLogType, logger.LogTypeApp),
		zap.String(logger.FieldEvent, "export.csv.aborted"),
		zap.String("file", filename),
		zap.Int("rows", s.rows),
		zap.Error(err),
	)
	panic(http.ErrAbortHandler)
}

func (s *csvStream) start() error {
	h := s.c.Writer.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+s.filename+`"`)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	s.c.Status(http.StatusOK)
	s.w = csv.NewWriter(s.c.Writer)
	return s.w.Write(s.header)
}

func (s *csvStream) write(cells []string) error {
	if s.w == nil {
		if err := s.start(); err != nil {
			return err
		}
	}
	if err := s.w.Write(cells); err != nil {
		return err
	}
	s.rows++
	if s.rows%csvFlushEvery == 0 {
		return s.flush()
	}
	return nil
}

// finish sends the header of an empty export and whatever is buffered.
func (s *csvStream) finish() error {
	if s.w == nil {
		if err := s.start(); err != nil {
			return err
		}
	}
	return s.flush()
}

func (s *csvStream) flush() error {
	s.w.Flush()
	if err := s.w.Error(); err != nil {
		return err
	}
	s.c.Writer.Flush()
	return nil
}

// csvText guards a text cell against spreadsheet formula injection.
func csvText(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// csvTime is t in RFC 3339 at WIB, or empty.
func csvTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(wib).Format(time.RFC3339)
}

// csvInt is an amount in minor units, or empty when unknown.
func csvInt(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

// csvDate is t as a WIB date for file names, or "all" for an open bound.
func csvDate(t *time.Time) string {
	if t == nil {
		return "all"
	}
	return t.In(wib).Format("20060102")
}
