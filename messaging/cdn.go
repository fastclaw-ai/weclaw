package messaging

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fastclaw-ai/weclaw/ilink"
)

const cdnBaseURL = "https://novac2c.cdn.weixin.qq.com/c2c"

// UploadedFile holds the result of a CDN upload.
type UploadedFile struct {
	DownloadParam string // encrypted query param for download
	AESKeyHex     string // hex-encoded AES key
	FileSize      int    // plaintext size
	CipherSize    int    // ciphertext size
}

// UploadFileToCDN encrypts and uploads a file to the WeChat CDN.
func UploadFileToCDN(ctx context.Context, client *ilink.Client, data []byte, toUserID string, mediaType int) (*UploadedFile, error) {
	// Generate random filekey and AES key
	filekey := make([]byte, 16)
	aeskey := make([]byte, 16)
	if _, err := rand.Read(filekey); err != nil {
		return nil, fmt.Errorf("generate filekey: %w", err)
	}
	if _, err := rand.Read(aeskey); err != nil {
		return nil, fmt.Errorf("generate aeskey: %w", err)
	}

	filekeyHex := hex.EncodeToString(filekey)
	aeskeyHex := hex.EncodeToString(aeskey)

	// Calculate MD5 of plaintext
	hash := md5.Sum(data)
	rawMD5 := hex.EncodeToString(hash[:])

	// Calculate ciphertext size (PKCS7 padding)
	cipherSize := aesECBPaddedSize(len(data))

	// Get upload URL from iLink API
	uploadReq := &ilink.GetUploadURLRequest{
		FileKey:     filekeyHex,
		MediaType:   mediaType,
		ToUserID:    toUserID,
		RawSize:     len(data),
		RawFileMD5:  rawMD5,
		FileSize:    cipherSize,
		NoNeedThumb: true,
		AESKey:      aeskeyHex,
		BaseInfo:    ilink.BaseInfo{},
	}

	uploadResp, err := client.GetUploadURL(ctx, uploadReq)
	if err != nil {
		return nil, fmt.Errorf("get upload URL: %w", err)
	}
	if uploadResp.Ret != 0 {
		return nil, fmt.Errorf("get upload URL failed: ret=%d errmsg=%s", uploadResp.Ret, uploadResp.ErrMsg)
	}

	// Encrypt data with AES-128-ECB
	encrypted, err := encryptAESECB(data, aeskey)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	// Upload to CDN: prefer server-provided full URL, fall back to param-based construction
	cdnURL := strings.TrimSpace(uploadResp.UploadFullURL)
	if cdnURL == "" {
		if uploadResp.UploadParam == "" {
			return nil, fmt.Errorf("getuploadurl returned no upload URL (need upload_full_url or upload_param)")
		}
		cdnURL = fmt.Sprintf("%s/upload?encrypted_query_param=%s&filekey=%s",
			cdnBaseURL, url.QueryEscape(uploadResp.UploadParam), url.QueryEscape(filekeyHex))
	}

	downloadParam, err := uploadToCDN(ctx, encrypted, cdnURL)
	if err != nil {
		return nil, fmt.Errorf("CDN upload: %w", err)
	}

	return &UploadedFile{
		DownloadParam: downloadParam,
		AESKeyHex:     aeskeyHex,
		FileSize:      len(data),
		CipherSize:    cipherSize,
	}, nil
}

// AESKeyToBase64 converts a hex AES key to base64(hex_string) format for message items.
// Used for outbound messages (file/voice/video).
func AESKeyToBase64(hexKey string) string {
	return base64.StdEncoding.EncodeToString([]byte(hexKey))
}

// HexKeyToBase64 converts a hex AES key to base64(raw_bytes) format.
// Used for image_item.aeskey which is raw hex.
func HexKeyToBase64(hexKey string) string {
	raw, err := hex.DecodeString(hexKey)
	if err != nil {
		return base64.StdEncoding.EncodeToString([]byte(hexKey))
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// DownloadFileFromCDN downloads and decrypts a file from the WeChat CDN.
func DownloadFileFromCDN(ctx context.Context, encryptQueryParam, aesKeyBase64 string) ([]byte, error) {
	// Parse AES key from base64. Two formats exist:
	//   - base64(raw 16 bytes)           → images (aes_key from media field)
	//   - base64(hex string of 16 bytes) → file / voice / video
	aesKey, err := parseAESKey(aesKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("parse AES key: %w", err)
	}

	// Download encrypted data from CDN
	downloadURL := fmt.Sprintf("%s/download?encrypted_query_param=%s",
		cdnBaseURL, url.QueryEscape(encryptQueryParam))

	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download from CDN: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errMsg := resp.Header.Get("X-Error-Message")
		if errMsg == "" {
			errMsg = string(body)
		}
		return nil, fmt.Errorf("CDN download HTTP %d: %s", resp.StatusCode, errMsg)
	}

	encrypted, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read CDN response: %w", err)
	}

	// Decrypt AES-128-ECB
	return decryptAESECB(encrypted, aesKey)
}

// parseAESKey decodes a base64-encoded AES key. Handles two formats:
//   - base64(raw 16 bytes) → used by images
//   - base64(32-char hex string) → used by file/voice/video
func parseAESKey(aesKeyBase64 string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(aesKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	if len(decoded) == 16 {
		// Already raw 16-byte key
		return decoded, nil
	}
	if len(decoded) == 32 {
		// Hex-encoded key: decode hex string to 16 raw bytes
		raw, err := hex.DecodeString(string(decoded))
		if err != nil {
			return nil, fmt.Errorf("hex decode: %w", err)
		}
		return raw, nil
	}
	return nil, fmt.Errorf("unexpected AES key length: %d bytes (expected 16 or 32)", len(decoded))
}

// decryptAESECB decrypts data encrypted with AES-128-ECB and removes PKCS7 padding.
func decryptAESECB(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext is not a multiple of block size")
	}

	plaintext := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += aes.BlockSize {
		block.Decrypt(plaintext[i:i+aes.BlockSize], ciphertext[i:i+aes.BlockSize])
	}

	// Remove PKCS7 padding
	if len(plaintext) == 0 {
		return plaintext, nil
	}
	padLen := int(plaintext[len(plaintext)-1])
	if padLen > aes.BlockSize || padLen == 0 {
		return nil, fmt.Errorf("invalid PKCS7 padding")
	}
	return plaintext[:len(plaintext)-padLen], nil
}

func uploadToCDN(ctx context.Context, encrypted []byte, cdnURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cdnURL, bytes.NewReader(encrypted))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("CDN upload HTTP %d: %s", resp.StatusCode, string(body))
	}

	downloadParam := resp.Header.Get("X-Encrypted-Param")
	if downloadParam == "" {
		return "", fmt.Errorf("CDN upload: missing X-Encrypted-Param header")
	}

	return downloadParam, nil
}

// encryptAESECB encrypts data using AES-128-ECB with PKCS7 padding.
func encryptAESECB(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// PKCS7 padding
	padLen := aes.BlockSize - (len(plaintext) % aes.BlockSize)
	padded := make([]byte, len(plaintext)+padLen)
	copy(padded, plaintext)
	for i := len(plaintext); i < len(padded); i++ {
		padded[i] = byte(padLen)
	}

	// ECB mode: encrypt each block independently
	encrypted := make([]byte, len(padded))
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(encrypted[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
	}

	return encrypted, nil
}

func aesECBPaddedSize(plaintextSize int) int {
	return (plaintextSize/aes.BlockSize + 1) * aes.BlockSize
}
