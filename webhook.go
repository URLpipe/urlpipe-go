package urlpipe

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Webhook signature headers.
const (
	TimestampHeader = "X-URLpipe-Timestamp"
	SignatureHeader = "X-URLpipe-Signature"
)

// DefaultWebhookTolerance is how old (or how far in the future) a delivery's
// timestamp may be when [VerifyWebhook] is given a tolerance of zero.
const DefaultWebhookTolerance = 5 * time.Minute

// WebhookEvent is the payload of a webhook delivery.
type WebhookEvent struct {
	Token     string    `json:"token"`
	Operation Operation `json:"operation"`
	// Labels are the request's labels; an empty map when it had none.
	Labels  map[string]string `json:"labels"`
	Success bool              `json:"success"`
	// Result is the operation's result as JSON; Data decodes it.
	Result json.RawMessage `json:"result"`
	// ResultURL links to a screenshot with no API key needed.
	ResultURL string `json:"result_url"`
	// Error is the failure message when Success is false.
	Error string `json:"error"`
	Meta  Meta   `json:"meta"`
}

// Data decodes Result the way the operation's method types it: a string
// for markdown, a *Screenshot, a *Metadata, and so on. It is nil for a
// failed analysis.
func (e *WebhookEvent) Data() (any, error) {
	return decodeJSONResult(e.Operation, e.Result, e.ResultURL)
}

// WebhookVerificationError says why a delivery failed verification. It
// matches [ErrWebhookVerification] with errors.Is.
type WebhookVerificationError struct {
	Reason string
}

func (e *WebhookVerificationError) Error() string {
	return "urlpipe: webhook verification failed: " + e.Reason
}

// Is matches [ErrWebhookVerification].
func (e *WebhookVerificationError) Is(target error) bool { return target == ErrWebhookVerification }

// ErrWebhookVerification matches every [*WebhookVerificationError].
var ErrWebhookVerification error = &kindSentinel{"webhook_verification"}

// VerifyWebhook checks a signed delivery and returns its payload. It needs
// no Client.
//
// rawBody must be the request body exactly as received: parsing the JSON
// and serializing it again changes the bytes, and the signature with them.
// header supplies X-URLpipe-Timestamp and X-URLpipe-Signature. secret is
// the project's signing secret (whsec_...), used as-is. A delivery whose
// timestamp is further than tolerance from now, in either direction, is
// refused; zero means [DefaultWebhookTolerance].
func VerifyWebhook(rawBody []byte, header http.Header, secret string, tolerance time.Duration) (*WebhookEvent, error) {
	return VerifyWebhookSignature(rawBody, header.Get(TimestampHeader), header.Get(SignatureHeader), secret, tolerance)
}

// VerifyWebhookSignature is [VerifyWebhook] for callers that hold the two
// header values rather than an http.Header.
func VerifyWebhookSignature(rawBody []byte, timestamp, signature, secret string, tolerance time.Duration) (*WebhookEvent, error) {
	if tolerance <= 0 {
		tolerance = DefaultWebhookTolerance
	}
	if secret == "" {
		return nil, &WebhookVerificationError{Reason: "no signing secret was given"}
	}
	timestamp = strings.TrimSpace(timestamp)
	if timestamp == "" {
		return nil, &WebhookVerificationError{Reason: "the " + TimestampHeader + " header is missing"}
	}
	if strings.TrimSpace(signature) == "" {
		return nil, &WebhookVerificationError{Reason: "the " + SignatureHeader + " header is missing"}
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return nil, &WebhookVerificationError{Reason: "the timestamp is not Unix seconds"}
	}
	age := time.Since(time.Unix(ts, 0))
	if age > tolerance || age < -tolerance {
		return nil, &WebhookVerificationError{Reason: "the timestamp is outside the tolerance of " + tolerance.String()}
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(rawBody)
	expected := mac.Sum(nil)

	matched := false
	for _, part := range strings.Split(signature, ",") {
		scheme, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || scheme != "v1" {
			continue
		}
		got, err := hex.DecodeString(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		if hmac.Equal(got, expected) {
			matched = true
		}
	}
	if !matched {
		return nil, &WebhookVerificationError{Reason: "no v1 signature matches the body"}
	}

	var event WebhookEvent
	if err := json.Unmarshal(rawBody, &event); err != nil {
		return nil, &WebhookVerificationError{Reason: "the body is not valid JSON: " + err.Error()}
	}
	if event.Labels == nil {
		event.Labels = map[string]string{}
	}
	return &event, nil
}
