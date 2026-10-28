package response

import (
	"os"
	"path/filepath"
	"slices"
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
					Enum []string `yaml:"enum"`
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
}
