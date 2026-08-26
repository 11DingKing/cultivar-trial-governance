package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

type Generator interface {
	New(prefix string) (string, error)
}

type Random struct{}

func (Random) New(prefix string) (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate random id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(buffer), nil
}

type Sequence struct {
	mu   sync.Mutex
	next int
}

func NewSequence(start int) *Sequence { return &Sequence{next: start} }

func (s *Sequence) New(prefix string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := fmt.Sprintf("%s_%06d", prefix, s.next)
	s.next++
	return value, nil
}
