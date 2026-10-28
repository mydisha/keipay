package postgres

import "strconv"

// itoa is a small helper for building positional placeholders ($1, $2, ...).
func itoa(i int) string { return strconv.Itoa(i) }
