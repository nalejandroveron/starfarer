package netcode

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"
)

// PSK protection authenticates a trusted group, not an individual account.
// Provision keys out of band. Random sender IDs derive separate AES-GCM keys.
type replayState struct {
	high, bits uint64
	seen       time.Time
	aead       cipher.AEAD
}
type Protector struct {
	secret  []byte
	sender  [16]byte
	counter uint64
	send    cipher.AEAD
	peers   map[[16]byte]*replayState
}

func NewProtector(secret []byte) (*Protector, error) {
	if len(secret) != 32 {
		return nil, errors.New("packet key must contain 32 bytes")
	}
	p := &Protector{secret: append([]byte(nil), secret...), peers: make(map[[16]byte]*replayState)}
	if _, err := rand.Read(p.sender[:]); err != nil {
		return nil, err
	}
	p.send = p.derive(p.sender)
	return p, nil
}
func (p *Protector) derive(id [16]byte) cipher.AEAD {
	mac := hmac.New(sha256.New, p.secret)
	mac.Write(id[:])
	block, _ := aes.NewCipher(mac.Sum(nil))
	aead, _ := cipher.NewGCM(block)
	return aead
}
func (p *Protector) Seal(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxDatagramSize || p.counter == ^uint64(0) {
		return nil, errors.New("invalid protected packet")
	}
	p.counter++
	header := make([]byte, 36)
	copy(header, "SFN2")
	copy(header[4:20], p.sender[:])
	binary.BigEndian.PutUint64(header[20:], p.counter)
	binary.BigEndian.PutUint64(header[28:], uint64(time.Now().UnixMilli()))
	nonce := make([]byte, 12)
	copy(nonce[4:], header[20:28])
	return p.send.Seal(header, nonce, data, header), nil
}
func (p *Protector) Open(data []byte, now time.Time) ([]byte, error) {
	invalid := errors.New("unauthenticated or replayed packet")
	if len(data) < 53 || len(data) > MaxDatagramSize+52 || string(data[:4]) != "SFN2" {
		return nil, invalid
	}
	var id [16]byte
	copy(id[:], data[4:20])
	seq := binary.BigEndian.Uint64(data[20:28])
	stamp := int64(binary.BigEndian.Uint64(data[28:36]))
	if stamp < now.Add(-30*time.Second).UnixMilli() || stamp > now.Add(30*time.Second).UnixMilli() {
		return nil, invalid
	}
	if seq == 0 {
		return nil, invalid
	}
	state := p.peers[id]
	var aead cipher.AEAD
	if state != nil {
		aead = state.aead
	} else {
		aead = p.derive(id)
	}
	nonce := make([]byte, 12)
	copy(nonce[4:], data[20:28])
	plain, err := aead.Open(nil, nonce, data[36:], data[:36])
	if err != nil {
		return nil, invalid
	}
	if state == nil {
		for key, peer := range p.peers {
			if now.Sub(peer.seen) > 2*time.Minute {
				delete(p.peers, key)
			}
		}
		if len(p.peers) >= 256 {
			return nil, errors.New("protected peer capacity reached")
		}
		state = &replayState{aead: aead}
		p.peers[id] = state
	}
	if seq > state.high {
		shift := seq - state.high
		if shift >= 64 {
			state.bits = 0
		} else {
			state.bits <<= shift
		}
		state.high = seq
		state.bits |= 1
	} else {
		age := state.high - seq
		if age >= 64 || state.bits&(uint64(1)<<age) != 0 {
			return nil, invalid
		}
		state.bits |= uint64(1) << age
	}
	state.seen = now
	return plain, nil
}
