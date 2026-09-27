package urlpipe_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/URLpipe/urlpipe-go"
)

func ExampleNewClient() {
	// Reads the key from URLPIPE_API_KEY.
	client, err := urlpipe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	_ = client

	// Or pass everything explicitly.
	client, err = urlpipe.NewClient(
		urlpipe.WithAPIKey(os.Getenv("MY_URLPIPE_KEY")),
		urlpipe.WithTimeout(2*time.Minute),
		urlpipe.WithMaxRetries(3),
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = client
}

func ExampleClient_Markdown() {
	client, err := urlpipe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	res, err := client.Markdown(context.Background(), "https://example.com", &urlpipe.Options{
		MaxAge:      urlpipe.MaxAgeString("1 hour"),
		PageOptions: map[string]any{"block_cookie_banners": true},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Data)
	fmt.Println("cache:", res.Meta.Cache, "credits left:", res.Meta.Quota.Remaining)
}

func ExampleClient_Screenshot() {
	client, err := urlpipe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	res, err := client.Screenshot(context.Background(), "https://example.com", &urlpipe.ScreenshotOptions{
		Screenshot: map[string]any{"format": "webp", "viewport_width": 390},
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := res.Data.Save("example.webp"); err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Data.MIMEType, res.Data.ResultURL)
}

func ExampleClient_Meta() {
	client, err := urlpipe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	res, err := client.Meta(context.Background(), "https://example.com", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Data.Title, res.Data.Language)
}

func ExampleClient_Scrape() {
	client, err := urlpipe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	res, err := client.Scrape(context.Background(), "https://example.com",
		[]urlpipe.Operation{urlpipe.OperationMarkdown, urlpipe.OperationMeta, urlpipe.OperationScreenshot}, nil)
	if err != nil {
		log.Fatal(err)
	}
	md, err := res.Data.Markdown()
	if err != nil {
		log.Println(err) // this operation failed; the others are still there
	}
	meta, _ := res.Data.Meta()
	fmt.Println(meta.Title, len(md))
}

func ExampleClient_Wait() {
	client, err := urlpipe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	// Accepted straight away; the audit runs in the background.
	accepted, err := client.Lighthouse(ctx, "https://example.com", &urlpipe.LighthouseOptions{
		Options: urlpipe.Options{Async: true},
		Device:  "desktop",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Later, or in another process: the operation hint types Data.
	res, err := client.Wait(ctx, accepted.Token, urlpipe.OperationLighthouse, nil)
	if err != nil {
		log.Fatal(err)
	}
	audit := res.Data.(*urlpipe.Lighthouse)
	fmt.Println(*audit.Categories["performance"].Score)
}

func Example_errors() {
	client, err := urlpipe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	_, err = client.Summarize(context.Background(), "https://example.com", nil)

	var quota *urlpipe.QuotaExceededError
	var apiErr *urlpipe.Error
	switch {
	case err == nil:
		fmt.Println("done")
	case errors.As(err, &quota):
		fmt.Printf("needs %d credits, %d left until %s\n", quota.Needed, quota.Limit-quota.Used, quota.ResetsAt)
	case errors.Is(err, urlpipe.ErrAnalysisFailed):
		fmt.Println("the page could not be analysed:", err)
	case errors.As(err, &apiErr):
		fmt.Println(apiErr.Kind, apiErr.Status, apiErr.Code, apiErr.Message)
	default:
		log.Fatal(err)
	}
}

func ExampleVerifyWebhook() {
	secret := "whsec_example"
	body := []byte(`{"token":"tok_1","operation":"markdown","labels":{},"success":true,"result":"# Example Domain","result_url":null,"error":null,"meta":{}}`)

	// What URLpipe sends: a timestamp and an HMAC of "<timestamp>.<body>".
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(body)))
	header := http.Header{}
	header.Set("X-URLpipe-Timestamp", ts)
	header.Set("X-URLpipe-Signature", "v1="+hex.EncodeToString(mac.Sum(nil)))

	event, err := urlpipe.VerifyWebhook(body, header, secret, 5*time.Minute)
	if err != nil {
		fmt.Println(err)
		return
	}
	data, _ := event.Data()
	fmt.Println(event.Token, event.Operation, data)
	// Output: tok_1 markdown # Example Domain
}

func ExampleVerifyWebhook_handler() {
	secret := os.Getenv("URLPIPE_WEBHOOK_SECRET")
	http.HandleFunc("/webhooks/urlpipe", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body) // the raw bytes, not re-encoded JSON
		if err != nil {
			http.Error(w, "unreadable body", http.StatusBadRequest)
			return
		}
		event, err := urlpipe.VerifyWebhook(body, r.Header, secret, 0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		log.Printf("result %s for %s ready", event.Token, event.Operation)
		w.WriteHeader(http.StatusNoContent)
	})
}
