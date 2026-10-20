package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewID returns a short, prefixed, collision-resistant identifier such as
// "inc_9f2c4b1d8a7e0356". Prefixes keep ids self-describing in logs and URLs.
func NewID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail in practice; panicking beats handing out
		// a predictable or empty id.
		panic(fmt.Sprintf("domain: generate id: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
