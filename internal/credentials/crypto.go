package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type envelope struct {
	Version      int    `json:"version"`
	KeyVersion   int    `json:"key_version"`
	WrappedDEK   string `json:"wrapped_dek"`
	DEKNonce     string `json:"dek_nonce"`
	PayloadNonce string `json:"payload_nonce"`
	Ciphertext   string `json:"ciphertext"`
}

// Encrypt uses a random per-record data key and authenticates the supplied
// metadata as additional authenticated data.
func Encrypt(provider MasterKeyProvider, metadata []byte, plaintext []byte) ([]byte, error) {
	version, master, err := provider.Current()
	if err != nil {
		return nil, err
	}
	if len(master) != 32 {
		return nil, fmt.Errorf("%w: invalid current key", ErrMasterKeyUnavailable)
	}
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("generate data key: %w", err)
	}
	wrapped, dekNonce, err := seal(master, nil, dek)
	if err != nil {
		return nil, fmt.Errorf("wrap data key: %w", err)
	}
	ciphertext, payloadNonce, err := seal(dek, metadata, plaintext)
	if err != nil {
		return nil, fmt.Errorf("encrypt credential: %w", err)
	}
	env := envelope{Version: 1, KeyVersion: version,
		WrappedDEK:   base64.StdEncoding.EncodeToString(wrapped),
		DEKNonce:     base64.StdEncoding.EncodeToString(dekNonce),
		PayloadNonce: base64.StdEncoding.EncodeToString(payloadNonce),
		Ciphertext:   base64.StdEncoding.EncodeToString(ciphertext)}
	return json.Marshal(env)
}

func Decrypt(provider MasterKeyProvider, metadata []byte, encoded []byte) ([]byte, error) {
	var env envelope
	if err := json.Unmarshal(encoded, &env); err != nil {
		return nil, fmt.Errorf("decode credential envelope: %w", err)
	}
	if env.Version != 1 {
		return nil, fmt.Errorf("unsupported credential envelope version %d", env.Version)
	}
	master, err := provider.Key(env.KeyVersion)
	if err != nil {
		return nil, err
	}
	wrapped, err := base64.StdEncoding.DecodeString(env.WrappedDEK)
	if err != nil {
		return nil, fmt.Errorf("decode wrapped key: %w", err)
	}
	dekNonce, err := base64.StdEncoding.DecodeString(env.DEKNonce)
	if err != nil {
		return nil, fmt.Errorf("decode key nonce: %w", err)
	}
	dek, err := open(master, nil, dekNonce, wrapped)
	if err != nil {
		return nil, fmt.Errorf("unwrap data key: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(env.PayloadNonce)
	if err != nil {
		return nil, fmt.Errorf("decode payload nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	return open(dek, metadata, nonce, ciphertext)
}

// Rewrap preserves plaintext confidentiality during master-key rotation.
func Rewrap(oldProvider, newProvider MasterKeyProvider, metadata, encoded []byte) ([]byte, error) {
	plain, err := Decrypt(oldProvider, metadata, encoded)
	if err != nil {
		return nil, err
	}
	return Encrypt(newProvider, metadata, plain)
}

func seal(key, aad, plaintext []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return gcm.Seal(nil, nonce, plaintext, aad), nonce, nil
}

func open(key, aad, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, aad)
}
