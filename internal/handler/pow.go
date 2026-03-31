package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const powActionCheckin = "checkin"

var ErrInvalidPoW = errors.New("PoW 校验失败或已过期，请刷新页面后重试")

type powChallenge struct {
	Action     string `json:"action"`
	LinuxDoID  string `json:"linux_do_id"`
	Nonce      string `json:"nonce"`
	Difficulty int    `json:"difficulty"`
	IssuedAt   int64  `json:"issued_at"`
	ExpiresAt  int64  `json:"expires_at"`
}

type powChallengeView struct {
	Payload    string
	Signature  string
	Difficulty int
	ExpiresAt  int64
}

func (a *App) issuePoWChallenge(linuxDoID string, now time.Time) (powChallengeView, error) {
	nonce, err := randomPoWNonce(24)
	if err != nil {
		return powChallengeView{}, fmt.Errorf("生成 PoW 挑战失败: %w", err)
	}

	challenge := powChallenge{
		Action:     powActionCheckin,
		LinuxDoID:  linuxDoID,
		Nonce:      nonce,
		Difficulty: a.config.CheckinPoWDifficulty,
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(a.config.CheckinPoWTTL).Unix(),
	}

	payloadBytes, err := json.Marshal(challenge)
	if err != nil {
		return powChallengeView{}, fmt.Errorf("编码 PoW 挑战失败: %w", err)
	}

	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return powChallengeView{
		Payload:    payload,
		Signature:  a.signPoWPayload(payload),
		Difficulty: challenge.Difficulty,
		ExpiresAt:  challenge.ExpiresAt,
	}, nil
}

func (a *App) verifyPoW(linuxDoID, payload, signature, counter, hash string, now time.Time) error {
	if !a.config.CheckinPoWEnabled {
		return nil
	}
	if payload == "" || signature == "" || counter == "" || hash == "" {
		return ErrInvalidPoW
	}
	if len(payload) > 1024 || len(signature) > 256 || len(counter) > 64 || len(hash) != sha256.Size*2 {
		return ErrInvalidPoW
	}
	if _, err := strconv.ParseUint(counter, 10, 64); err != nil {
		return ErrInvalidPoW
	}
	if !a.verifyPoWSignature(payload, signature) {
		return ErrInvalidPoW
	}

	challenge, err := decodePoWPayload(payload)
	if err != nil {
		return ErrInvalidPoW
	}
	if challenge.Action != powActionCheckin || challenge.LinuxDoID != linuxDoID {
		return ErrInvalidPoW
	}
	if challenge.Difficulty != a.config.CheckinPoWDifficulty || challenge.Difficulty < 0 {
		return ErrInvalidPoW
	}
	nowUnix := now.Unix()
	if challenge.IssuedAt <= 0 || challenge.ExpiresAt <= challenge.IssuedAt || challenge.ExpiresAt <= nowUnix {
		return ErrInvalidPoW
	}

	sum := sha256.Sum256([]byte(payload + ":" + counter))
	expectedHash := hex.EncodeToString(sum[:])
	if !strings.EqualFold(hash, expectedHash) {
		return ErrInvalidPoW
	}
	if !hasLeadingZeroBits(sum[:], challenge.Difficulty) {
		return ErrInvalidPoW
	}
	return nil
}

func (a *App) signPoWPayload(payload string) string {
	mac := hmac.New(sha256.New, a.config.JWTSecret)
	mac.Write([]byte("pow:"))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *App) verifyPoWSignature(payload, signature string) bool {
	actual, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, a.config.JWTSecret)
	mac.Write([]byte("pow:"))
	mac.Write([]byte(payload))
	expected := mac.Sum(nil)
	return hmac.Equal(actual, expected)
}

func decodePoWPayload(payload string) (powChallenge, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return powChallenge{}, err
	}

	var challenge powChallenge
	if err := json.Unmarshal(decoded, &challenge); err != nil {
		return powChallenge{}, err
	}
	return challenge, nil
}

func hasLeadingZeroBits(sum []byte, bits int) bool {
	if bits <= 0 {
		return true
	}
	fullBytes := bits / 8
	remainingBits := bits % 8
	if fullBytes > len(sum) || fullBytes == len(sum) && remainingBits > 0 {
		return false
	}
	for index := 0; index < fullBytes; index++ {
		if sum[index] != 0 {
			return false
		}
	}
	if remainingBits == 0 {
		return true
	}
	return sum[fullBytes]>>(8-remainingBits) == 0
}

func randomPoWNonce(length int) (string, error) {
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
