package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// 错误分类：调用方据此区分「密钥不可用」与「密文被篡改/密钥不对」。
var (
	ErrUnknownKeyVersion = errors.New("crypto: unknown master key version")
	ErrDecrypt           = errors.New("crypto: decrypt failed (wrong key or tampered ciphertext)")
)

// Sealed 是加密后的凭据：nonce 与密文分离存放（design D7）。
type Sealed struct {
	Nonce      []byte
	Ciphertext []byte
	KeyVersion int
}

// Ring 持有当前主密钥与历史主密钥，支持按版本解密与轮换。
// 主密钥来自环境变量 / KMS / Vault，绝不入库（design D7）。
type Ring struct {
	current int
	keys    map[int][]byte
}

// NewRing 构造密钥环。keys 的键是 key_version，值必须是 32 字节的 AES-256 密钥。
func NewRing(current int, keys map[int][]byte) (*Ring, error) {
	if len(keys) == 0 {
		return nil, errors.New("crypto: at least one master key is required")
	}
	copied := make(map[int][]byte, len(keys))
	for version, key := range keys {
		if version < 1 {
			return nil, fmt.Errorf("crypto: key version must be >= 1, got %d", version)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("crypto: master key v%d must be 32 bytes, got %d", version, len(key))
		}
		buf := make([]byte, len(key))
		copy(buf, key)
		copied[version] = buf
	}
	if _, ok := copied[current]; !ok {
		return nil, fmt.Errorf("crypto: current key version %d not present in key ring", current)
	}
	return &Ring{current: current, keys: copied}, nil
}

// ParseKey 解析 base64 编码的 32 字节主密钥。
func ParseKey(s string) ([]byte, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return nil, errors.New("crypto: master key is empty")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("crypto: master key must be base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: master key must decode to 32 bytes, got %d", len(key))
	}
	return key, nil
}

// Version 返回新写入数据使用的密钥版本。
func (r *Ring) Version() int { return r.current }

// NeedsRotation 判断某个密文是否需要用当前主密钥重加密。
func (r *Ring) NeedsRotation(keyVersion int) bool { return keyVersion != r.current }

// Seal 用当前主密钥加密。每条凭据使用独立随机 nonce。
func (r *Ring) Seal(plaintext []byte) (Sealed, error) {
	return r.sealWith(r.current, plaintext)
}

func (r *Ring) sealWith(version int, plaintext []byte) (Sealed, error) {
	key, ok := r.keys[version]
	if !ok {
		return Sealed{}, fmt.Errorf("%w: v%d", ErrUnknownKeyVersion, version)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return Sealed{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Sealed{}, fmt.Errorf("crypto: read nonce: %w", err)
	}
	return Sealed{
		Nonce:      nonce,
		Ciphertext: gcm.Seal(nil, nonce, plaintext, aad(version)),
		KeyVersion: version,
	}, nil
}

// Open 按密文携带的 key_version 解密。
func (r *Ring) Open(s Sealed) ([]byte, error) {
	key, ok := r.keys[s.KeyVersion]
	if !ok {
		return nil, fmt.Errorf("%w: v%d", ErrUnknownKeyVersion, s.KeyVersion)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(s.Nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("%w: nonce size %d", ErrDecrypt, len(s.Nonce))
	}
	plaintext, err := gcm.Open(nil, s.Nonce, s.Ciphertext, aad(s.KeyVersion))
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

// Rotate 用当前主密钥重新加密，返回新的密文与 key_version。
// 调用方必须在同一事务内用结果覆盖旧记录，保证「旧凭据立即失效」。
func (r *Ring) Rotate(s Sealed) (Sealed, error) {
	plaintext, err := r.Open(s)
	if err != nil {
		return Sealed{}, err
	}
	return r.Seal(plaintext)
}

// Fingerprint 返回凭据的不可逆指纹，用于轮换比对与展示（不泄露明文）。
func Fingerprint(plaintext []byte) string {
	sum := sha256.Sum256(plaintext)
	return hex.EncodeToString(sum[:16])
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}
	return gcm, nil
}

// aad 把 key_version 绑定进认证范围，避免用其他版本密钥解出的密文被误接受。
func aad(version int) []byte {
	return []byte("kubedeck/credential/v" + strconv.Itoa(version))
}
