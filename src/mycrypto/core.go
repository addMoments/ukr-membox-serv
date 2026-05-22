package mycrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"math/big"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func Rand_1() (float64, error) {
	bytes := make([]byte, 8)

	_, err := rand.Read(bytes)
	if err != nil {
		fmt.Println("Error generating random bytes:", err)
		return 0.0, err
	}

	// Convert the random bytes to a float64
	floatValue := float64(binary.BigEndian.Uint64(bytes))

	// Normalize the float value to be between 0 and 1
	normalizedValue := floatValue / math.MaxUint64

	return normalizedValue, nil
}

func Rand_Int(min, max int) (int, error) {
	rangeSize := max - min + 1
	randomValue, err := rand.Int(rand.Reader, big.NewInt(int64(rangeSize)))
	if err != nil {
		return 0, err
	}
	result := min + int(randomValue.Int64())
	return result, nil
}

func Rand_Str(length int, charset string) string {
	var charset_ar []string
	var res_str string
	if charset == "" {
		charset_ar = strings.Split("qwertyuiopasdfghjklzxcvbnmQWERTYUIOPASDFGHJKLZXCVBNM1234567890", "")
	} else {
		charset_ar = strings.Split(charset, "")
	}

	charset_len := len(charset_ar)
	for i := 0; i < length; i++ {
		r_int, err := Rand_Int(0, charset_len-1)
		if err != nil {
			i--
			continue
		}
		res_str += charset_ar[r_int]
	}

	return res_str
}

func Encrypt(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], []byte(plaintext))

	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

func Decrypt(ciphertext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	ciphertextBytes, err := base64.URLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	if len(ciphertextBytes) < aes.BlockSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	iv := ciphertextBytes[:aes.BlockSize]
	ciphertextBytes = ciphertextBytes[aes.BlockSize:]

	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(ciphertextBytes, ciphertextBytes)

	return string(ciphertextBytes), nil
}

func QuickHash(input string) string {
	// Convert the integer to a byte slice
	inputBytes := []byte(input)

	// Create a new FNV-1a 64-bit hash
	hasher := fnv.New64a()

	// Write the byte slice to the hasher
	hasher.Write(inputBytes)

	// Get the 64-bit hash value
	hashValue := hasher.Sum64()

	// Convert the hash value to a hexadecimal string
	return fmt.Sprintf("%x", hashValue)
}

func HashPassword(password string) (string, error) {
	// Convert password string to byte slice
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14) // 14 is a reasonable cost
	return string(bytes), err
}

func CheckPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}
