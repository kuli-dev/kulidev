// Package auth pairs devices with a workspace: a short-lived one-time code becomes a
// long-lived, revocable session token. Only token hashes are stored.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Device is one paired browser or phone.
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Created   time.Time `json:"created"`
	LastSeen  time.Time `json:"lastSeen"`
	TokenHash string    `json:"tokenHash"`
}

// Public strips the secret hash.
func (d Device) Public() Device {
	d.TokenHash = ""
	return d
}

const (
	// CodeTTL is how long a pairing code stays valid.
	CodeTTL = 10 * time.Minute
	// maxFailures wrong codes within failureWindow lock pairing for that window.
	maxFailures   = 10
	failureWindow = time.Minute
	codeAlphabet  = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O, 1/I
)

var (
	ErrBadCode     = errors.New("that pairing code is wrong or has expired")
	ErrRateLimited = errors.New("too many wrong codes; wait a minute and try again")
)

// Store keeps devices in a JSON file (mode 0600) and pairing codes in memory.
type Store struct {
	path string
	now  func() time.Time

	mu       sync.Mutex
	devices  []Device
	codes    map[string]time.Time // code hash → expiry
	failures []time.Time
	dirty    time.Time // last persisted LastSeen update
}

// Open loads (or starts) the device list at path.
func Open(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now, codes: map[string]time.Time{}}
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &s.devices); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the OS RNG failing is not recoverable
	}
	return b
}

// NewCode returns a one-time pairing code like "K7QM-2XRP" (40 bits).
func (s *Store) NewCode() string {
	b := randomBytes(8)
	var sb strings.Builder
	for i, c := range b {
		if i == 4 {
			sb.WriteByte('-')
		}
		sb.WriteByte(codeAlphabet[int(c)%len(codeAlphabet)])
	}
	code := sb.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for h, exp := range s.codes {
		if now.After(exp) {
			delete(s.codes, h)
		}
	}
	s.codes[hash(normalize(code))] = now.Add(CodeTTL)
	return code
}

func normalize(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	return strings.NewReplacer("-", "", " ", "").Replace(code)
}

// Pair redeems a code for a new device and returns its session token.
func (s *Store) Pair(code, name string) (string, Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	recent := s.failures[:0]
	for _, t := range s.failures {
		if now.Sub(t) < failureWindow {
			recent = append(recent, t)
		}
	}
	s.failures = recent
	if len(s.failures) >= maxFailures {
		return "", Device{}, ErrRateLimited
	}
	h := hash(normalize(code))
	exp, ok := s.codes[h]
	if !ok || now.After(exp) {
		s.failures = append(s.failures, now)
		return "", Device{}, ErrBadCode
	}
	delete(s.codes, h) // single use

	name = strings.TrimSpace(name)
	if name == "" {
		name = "Unnamed device"
	}
	if len(name) > 60 {
		name = name[:60]
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes(32))
	d := Device{
		ID: hex.EncodeToString(randomBytes(6)), Name: name, Created: now, LastSeen: now, TokenHash: hash(token),
	}
	s.devices = append(s.devices, d)
	if err := s.save(); err != nil {
		s.devices = s.devices[:len(s.devices)-1]
		return "", Device{}, err
	}
	return token, d, nil
}

// Check returns the device a token belongs to.
func (s *Store) Check(token string) (Device, bool) {
	if token == "" {
		return Device{}, false
	}
	h := hash(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.devices {
		if subtle.ConstantTimeCompare([]byte(s.devices[i].TokenHash), []byte(h)) == 1 {
			now := s.now()
			s.devices[i].LastSeen = now
			if now.Sub(s.dirty) > time.Minute { // persist "last seen" at most once a minute
				s.dirty = now
				s.save()
			}
			return s.devices[i], true
		}
	}
	return Device{}, false
}

// List returns paired devices without their token hashes.
func (s *Store) List() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Device, len(s.devices))
	for i, d := range s.devices {
		out[i] = d.Public()
	}
	return out
}

// Revoke unpairs a device; its token stops working immediately.
func (s *Store) Revoke(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range s.devices {
		if d.ID == id {
			s.devices = append(s.devices[:i], s.devices[i+1:]...)
			s.save()
			return true
		}
	}
	return false
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s.devices, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
