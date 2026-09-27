package urlpipe

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

const (
	secret    = "whsec_test_secret"
	oldSecret = "whsec_old_secret"
)

var deliveryBody = []byte(`{"token":"tok_1","operation":"markdown","labels":{"client":"acme"},"success":true,"result":"# Example Domain","result_url":null,"error":null,"meta":{"cache":"miss","cache_age":null,"processing_time_ms":4182,"quota":{"cost":1,"limit":1000,"remaining":"unlimited","overage":0,"resets_at":"2026-08-31T23:59:59Z","concurrency_limit":3}}}`)

func sign(key string, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func headers(ts, sig string) http.Header {
	h := http.Header{}
	h.Set("X-URLpipe-Timestamp", ts)
	h.Set("X-URLpipe-Signature", sig)
	return h
}

func now() string { return strconv.FormatInt(time.Now().Unix(), 10) }

func TestVerifyWebhookValid(t *testing.T) {
	ts := now()
	ev, err := VerifyWebhook(deliveryBody, headers(ts, sign(secret, ts, deliveryBody)), secret, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Token != "tok_1" || ev.Operation != OperationMarkdown || !ev.Success || ev.Labels["client"] != "acme" || ev.ResultURL != "" || ev.Error != "" {
		t.Errorf("event = %+v", ev)
	}
	if ev.Meta.Cache != "miss" || ev.Meta.CacheAge != nil || *ev.Meta.ProcessingTimeMs != 4182 ||
		*ev.Meta.Quota.Cost != 1 || ev.Meta.Quota.Limit.Value != 1000 || !ev.Meta.Quota.Remaining.Unlimited ||
		ev.Meta.ConcurrencyLimit.Value != 3 || ev.Meta.Quota.ResetsAt == nil {
		t.Errorf("meta = %+v quota %+v", ev.Meta, ev.Meta.Quota)
	}
	d, err := ev.Data()
	if err != nil || d != "# Example Domain" {
		t.Errorf("data = %#v %v", d, err)
	}
}

func TestVerifyWebhookDecodesAScreenshot(t *testing.T) {
	body := []byte(`{"token":"t","operation":"screenshot","labels":{},"success":true,"result":"` + base64.StdEncoding.EncodeToString(pngBytes) + `","result_url":"https://urlpipe.dev/r/x.png","error":null,"meta":{}}`)
	ts := now()
	ev, err := VerifyWebhook(body, headers(ts, sign(secret, ts, body)), secret, 0)
	if err != nil {
		t.Fatal(err)
	}
	d, err := ev.Data()
	shot, ok := d.(*Screenshot)
	if err != nil || !ok || shot.MIMEType != "image/png" || shot.ResultURL != "https://urlpipe.dev/r/x.png" {
		t.Errorf("data = %#v %v", d, err)
	}
}

func TestVerifyWebhookRejects(t *testing.T) {
	ts := now()
	good := sign(secret, ts, deliveryBody)
	stale := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	future := strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
	tampered := append([]byte(nil), deliveryBody...)
	tampered[len(tampered)-2] = ' '

	for _, tc := range []struct {
		name   string
		body   []byte
		header http.Header
		secret string
	}{
		{"tampered body", tampered, headers(ts, good), secret},
		{"wrong secret", deliveryBody, headers(ts, good), "whsec_wrong"},
		{"stale timestamp", deliveryBody, headers(stale, sign(secret, stale, deliveryBody)), secret},
		{"future timestamp", deliveryBody, headers(future, sign(secret, future, deliveryBody)), secret},
		{"timestamp swapped", deliveryBody, headers(strconv.FormatInt(time.Now().Unix()-1, 10), good), secret},
		{"only an unknown scheme", deliveryBody, headers(ts, "v2="+good[3:]), secret},
		{"missing signature", deliveryBody, headers(ts, ""), secret},
		{"missing timestamp", deliveryBody, headers("", good), secret},
		{"non-numeric timestamp", deliveryBody, headers("yesterday", good), secret},
		{"empty secret", deliveryBody, headers(ts, good), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := VerifyWebhook(tc.body, tc.header, tc.secret, 5*time.Minute)
			var ve *WebhookVerificationError
			if !errors.As(err, &ve) || ve.Reason == "" || !errors.Is(err, ErrWebhookVerification) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestVerifyWebhookDuringRotation(t *testing.T) {
	ts := now()
	// The new secret signs first, the old one second; an endpoint still on
	// the old secret must match the second value.
	sig := sign("whsec_new", ts, deliveryBody) + "," + sign(oldSecret, ts, deliveryBody)
	if _, err := VerifyWebhook(deliveryBody, headers(ts, sig), oldSecret, 0); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyWebhookIgnoresUnknownSchemes(t *testing.T) {
	ts := now()
	sig := "v2=deadbeef, v0=nothex," + sign(secret, ts, deliveryBody)
	if _, err := VerifyWebhook(deliveryBody, headers(ts, sig), secret, 0); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	ts := now()
	ev, err := VerifyWebhookSignature(deliveryBody, ts, sign(secret, ts, deliveryBody), secret, time.Minute)
	if err != nil || ev.Token != "tok_1" {
		t.Fatalf("event %+v, err %v", ev, err)
	}
}

func TestVerifyWebhookFailure(t *testing.T) {
	body := []byte(`{"token":"tok_1","operation":"meta","labels":{},"success":false,"result":null,"result_url":null,"error":"The request timed out.","meta":{"cache":"miss","quota":{"cost":0}}}`)
	ts := now()
	ev, err := VerifyWebhook(body, headers(ts, sign(secret, ts, body)), secret, 0)
	if err != nil {
		t.Fatal(err)
	}
	d, err := ev.Data()
	if ev.Success || ev.Error != "The request timed out." || d != nil || err != nil {
		t.Errorf("event = %+v data %#v %v", ev, d, err)
	}
}

func TestVerifyWebhookWithoutLabels(t *testing.T) {
	body := []byte(`{"token":"t","operation":"markdown","success":true,"result":"x","meta":{}}`)
	ts := now()
	ev, err := VerifyWebhook(body, headers(ts, sign(secret, ts, body)), secret, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Labels == nil || len(ev.Labels) != 0 {
		t.Errorf("labels = %#v, want an empty map", ev.Labels)
	}
}
