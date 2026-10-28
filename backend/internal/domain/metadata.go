package domain

import "encoding/json"

// Metadata is an app's own flat key-value data. Values are string,
// json.Number, bool or nil, so numbers keep their exact digits.
type Metadata map[string]any

// MarshalJSON encodes nil or empty metadata as {} rather than null.
func (m Metadata) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]any(m))
}
