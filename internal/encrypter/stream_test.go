package encrypter

import (
	"bytes"
	"io"
	"testing"
)

func TestEncryptAndDecryptReader(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	plain := []byte("arquivo de teste para criptografia em stream")

	encrypted, err := EncryptReader(bytes.NewReader(plain), key)
	if err != nil {
		t.Fatalf("EncryptReader error: %v", err)
	}

	encryptedBytes, err := io.ReadAll(encrypted)
	if err != nil {
		t.Fatalf("reading encrypted bytes: %v", err)
	}
	if bytes.Contains(encryptedBytes, plain) {
		t.Fatal("ciphertext contains plaintext")
	}

	decrypted, err := DecryptReader(bytes.NewReader(encryptedBytes), key)
	if err != nil {
		t.Fatalf("DecryptReader error: %v", err)
	}
	decryptedBytes, err := io.ReadAll(decrypted)
	if err != nil {
		t.Fatalf("reading decrypted bytes: %v", err)
	}
	if !bytes.Equal(decryptedBytes, plain) {
		t.Fatalf("unexpected plaintext after decrypt: got %q", string(decryptedBytes))
	}
}

func TestWrapAndUnwrapVaultKey(t *testing.T) {
	master := bytes.Repeat([]byte{0x51}, 32)
	vault := bytes.Repeat([]byte{0x99}, 32)

	nonce, cipher, err := WrapVaultKey(master, vault)
	if err != nil {
		t.Fatalf("WrapVaultKey error: %v", err)
	}

	unwrapped, err := UnwrapVaultKey(master, nonce, cipher)
	if err != nil {
		t.Fatalf("UnwrapVaultKey error: %v", err)
	}
	if !bytes.Equal(unwrapped, vault) {
		t.Fatal("vault key mismatch after unwrap")
	}
}
