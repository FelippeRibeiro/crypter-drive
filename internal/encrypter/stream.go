package encrypter

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

// EncryptReader encrypts src using AES-CTR and prepends IV bytes.
func EncryptReader(src io.Reader, key []byte) (io.Reader, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}

	pipeReader, pipeWriter := io.Pipe()
	go func() {
		defer pipeWriter.Close()

		if _, err := pipeWriter.Write(iv); err != nil {
			_ = pipeWriter.CloseWithError(err)
			return
		}

		stream := cipher.NewCTR(block, iv)
		streamWriter := &cipher.StreamWriter{S: stream, W: pipeWriter}
		if _, err := io.Copy(streamWriter, src); err != nil {
			_ = pipeWriter.CloseWithError(fmt.Errorf("encrypt stream copy: %w", err))
			return
		}
	}()

	return pipeReader, nil
}

// DecryptReader reverses EncryptReader output (first 16 bytes are IV).
func DecryptReader(src io.Reader, key []byte) (io.Reader, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(src, iv); err != nil {
		return nil, err
	}

	pipeReader, pipeWriter := io.Pipe()
	go func() {
		defer pipeWriter.Close()

		stream := cipher.NewCTR(block, iv)
		streamReader := &cipher.StreamReader{S: stream, R: src}
		if _, err := io.Copy(pipeWriter, streamReader); err != nil {
			_ = pipeWriter.CloseWithError(fmt.Errorf("decrypt stream copy: %w", err))
			return
		}
	}()

	return pipeReader, nil
}
