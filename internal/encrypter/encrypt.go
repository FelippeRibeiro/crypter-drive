package encrypter

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"
	"os"
)

func EncryptFile(filename string, key []byte) error {
	// 1. Ler o conteúdo do arquivo original
	plaintext, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	// 2. Criar a instância do bloco AES
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	// 3. Criar o modo GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	// 4. Criar um Nonce aleatório
	nonce := make([]byte, gcm.NonceSize())

	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}

	// 5. Criptografar (o Seal anexa o resultado ao prefixo, aqui o nonce)
	// O formato será: [nonce][conteúdo_criptografado]
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	// 6. Escrever no arquivo .enc
	return os.WriteFile(filename+".enc", ciphertext, 0644)
}
