package encryption

import (
	"testing"
)

func TestEncryptAndDecrypt(t *testing.T) {
	es := NewMockService()
	tests := []string{
		"Q_Qesb1Z2hA7H94iXu3_buJeQ7416",
	}

	for _, val := range tests {
		encKey, encVal, err := es.Encrypt(val)
		if err != nil {
			t.Errorf("Error encrypting string %v", err)
		}

		res, err := es.Decrypt(encKey, encVal)
		if err != nil {
			t.Errorf("Error decrypting string %v", err)
		}

		if res != val {
			t.Error("Encrypted and decrypted values do not match")
		}
	}
}
