package delivery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strconv"
	"strings"
)

// Signer keeps the active signing secrets in configuration order. It is a
// small value type so a listener can reuse its signing policy on every retry.
type Signer struct {
	secrets [][]byte
}

// String and GoString deliberately omit the secret bytes so ordinary logging
// and diagnostics cannot render a signing credential.
func (s Signer) String() string   { return "Signer{secrets:<redacted>}" }
func (s Signer) GoString() string { return "delivery.Signer{secrets:<redacted>}" }

// LogValue makes the redaction explicit for both slog handlers. JSON handlers do not
// consult Stringer, so this method is the handler-independent containment boundary.
func (s Signer) LogValue() slog.Value { return slog.StringValue(s.String()) }

// NewSigner copies configured secret strings into a signer without exposing
// them through formatting or error values.
func NewSigner(secrets []string) Signer {
	values := make([][]byte, len(secrets))
	for index, secret := range secrets {
		values[index] = []byte(secret)
	}
	return Signer{secrets: values}
}

// Header emits the complete X-Pg-Noty-Signature value for one attempt.
func (s Signer) Header(timestamp int64, body []byte) string {
	return SignAll(s.secrets, timestamp, body)
}

// Verify accepts any currently configured secret, supporting zero-downtime
// rotation while each candidate is compared with hmac.Equal.
func (s Signer) Verify(timestamp int64, body []byte, header string) bool {
	for _, secret := range s.secrets {
		if VerifyHeader(secret, timestamp, body, header) {
			return true
		}
	}
	return false
}

// Sign computes lowercase HMAC-SHA256 over the decimal Unix timestamp, a dot,
// and the exact body bytes. The body is not copied into a new serialization.
func Sign(secret []byte, timestamp int64, body []byte) string {
	return hex.EncodeToString(signatureDigest(secret, timestamp, body))
}

// SignAll emits one v1 value per secret, retaining configuration order.
func SignAll(secrets [][]byte, timestamp int64, body []byte) string {
	values := make([]string, len(secrets))
	for index, secret := range secrets {
		values[index] = "v1=" + Sign(secret, timestamp, body)
	}
	return strings.Join(values, ",")
}

// Verify checks a raw lowercase hex digest with constant-time comparison.
func Verify(secret []byte, timestamp int64, body []byte, candidate string) bool {
	if len(candidate) != sha256.Size*2 || !isLowerHex(candidate) {
		return false
	}
	want, err := hex.DecodeString(candidate)
	if err != nil {
		return false
	}
	return hmac.Equal(signatureDigest(secret, timestamp, body), want)
}

func signatureDigest(secret []byte, timestamp int64, body []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return mac.Sum(nil)
}

func isLowerHex(value string) bool {
	if len(value) == 0 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// VerifyHeader accepts a comma-separated v1 signature header and verifies it
// against one configured secret. Unknown terms and malformed values fail closed.
func VerifyHeader(secret []byte, timestamp int64, body []byte, header string) bool {
	matched := false
	for _, term := range strings.Split(header, ",") {
		if !strings.HasPrefix(term, "v1=") {
			return false
		}
		candidate := strings.TrimPrefix(term, "v1=")
		if len(candidate) != sha256.Size*2 || !isLowerHex(candidate) {
			return false
		}
		if Verify(secret, timestamp, body, candidate) {
			matched = true
		}
	}
	return matched
}
