package utils

import (
	"crypto/rand"
	"encoding/base64"
)

const (
	constAuthTokenLengthBytes = 20
)

func generateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func generateRandomString(s int) (string, error) {
	b, err := generateRandomBytes(s)
	return base64.URLEncoding.EncodeToString(b), err
}

// GenerateAuthToken generates a string token for use in URLs.
func GenerateAuthToken() (string, error) {
	return generateRandomString(constAuthTokenLengthBytes)
}
