package response

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// The domain error codes and the ErrorCode enum in the contract must not drift.
func TestErrorCodesMatchOpenAPI(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec struct {
		Components struct {
			Schemas struct {
				ErrorCode struct {
					Description string   `yaml:"description"`
					Enum        []string `yaml:"enum"`
				} `yaml:"ErrorCode"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	want := spec.Components.Schemas.ErrorCode.Enum
	if len(want) == 0 {
		t.Fatal("spec has no ErrorCode enum")
	}
	var got []string
	for _, c := range domain.ErrorCodes() {
		got = append(got, string(c))
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("domain codes %v\nspec codes   %v", got, want)
	}

	// The status column of the table in the enum's description equals the
	// status each code is sent with.
	row := regexp.MustCompile("^\\| `([a-z_]+)` \\| ([0-9]{3}) \\|")
	rows := 0
	for _, line := range strings.Split(spec.Components.Schemas.ErrorCode.Description, "\n") {
		m := row.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		rows++
		status, _ := strconv.Atoi(m[2])
		if sent := statusByCode[domain.ErrorCode(m[1])]; sent != status {
			t.Errorf("%s: status %d, spec table says %d (%s)", m[1], sent, status, http.StatusText(status))
		}
	}
	if rows != len(want) {
		t.Fatalf("spec table has %d rows, enum has %d codes", rows, len(want))
	}
}
