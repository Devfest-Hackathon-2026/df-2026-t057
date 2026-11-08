// Package id は識別子を生成する。
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
)

// Generator は新しい識別子を返す。
type Generator interface {
	New() string
}

// Random は128ビットの乱数を16進数で返す。
type Random struct{}

func (Random) New() string {
	var b [16]byte
	// crypto/rand.Read は失敗しない（Go 1.24以降、失敗時はプロセスを止める）。
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Sequence はテスト用に、予測できる識別子を返す。
type Sequence struct {
	Prefix string
	n      atomic.Int64
}

func (s *Sequence) New() string { return fmt.Sprintf("%s%032d", s.Prefix, s.n.Add(1)) }
