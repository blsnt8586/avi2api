package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

type Cipher struct{ aead cipher.AEAD }

func New(key []byte) (*Cipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(plain string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (c *Cipher) Decrypt(encoded string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	if len(raw) < c.aead.NonceSize() {
		return "", errors.New("encrypted value is truncated")
	}
	nonce, body := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]
	plain, err := c.aead.Open(nil, nonce, body, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
