package http

import (
	"fmt"
	"io"
	"log"
	"strings"
	"syscall/js"
	"time"

	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/client"
)

// RegisterHandlers registers HTTP-related handlers for JavaScript interaction
func RegisterHandlers() {
	js.Global().Set("makeNetbirdRequest", js.FuncOf(makeNetbirdRequest))
	js.Global().Set("netbirdProxyRequest", js.FuncOf(netbirdProxyRequest))
	log.Println("HTTP client registered for JavaScript")
}

func makeNetbirdRequest(this js.Value, args []js.Value) interface{} {
	handler := js.FuncOf(func(this js.Value, promiseArgs []js.Value) interface{} {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			if len(args) < 1 {
				reject.Invoke("URL is required")
				return
			}

			url := args[0].String()
			response, err := makeHTTPRequest(url)
			if err != nil {
				reject.Invoke(err.Error())
				return
			}

			resolve.Invoke(response)
		}()

		return nil
	})

	promiseConstructor := js.Global().Get("Promise")
	return promiseConstructor.New(handler)
}

func netbirdProxyRequest(this js.Value, args []js.Value) interface{} {
	handler := js.FuncOf(func(this js.Value, promiseArgs []js.Value) interface{} {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			if len(args) < 1 {
				reject.Invoke("URL is required")
				return
			}

			url := args[0].String()
			nbClient := client.GetClient()

			if nbClient == nil {
				reject.Invoke("NetBird client not initialized")
				return
			}

			httpClient := nbClient.NewHTTPClient()
			httpClient.Timeout = 30 * time.Second

			resp, err := httpClient.Get(url)
			if err != nil {
				reject.Invoke(fmt.Sprintf("Request failed: %v", err))
				return
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
			if err != nil {
				reject.Invoke(fmt.Sprintf("Failed to read response: %v", err))
				return
			}

			result := js.Global().Get("Object").New()
			result.Set("status", resp.StatusCode)
			result.Set("statusText", resp.Status)
			result.Set("body", string(body))

			headers := js.Global().Get("Object").New()
			for key, values := range resp.Header {
				if len(values) > 0 {
					headers.Set(strings.ToLower(key), values[0])
				}
			}
			result.Set("headers", headers)

			resolve.Invoke(result)
		}()

		return nil
	})

	promiseConstructor := js.Global().Get("Promise")
	return promiseConstructor.New(handler)
}

func makeHTTPRequest(url string) (string, error) {
	nbClient := client.GetClient()
	if nbClient == nil {
		return "", fmt.Errorf("NetBird client not initialized")
	}

	httpClient := nbClient.NewHTTPClient()
	httpClient.Timeout = 30 * time.Second

	resp, err := httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	response := fmt.Sprintf("Status: %s\nContent-Type: %s\nContent-Length: %d\n\n%s",
		resp.Status,
		resp.Header.Get("Content-Type"),
		len(body),
		string(body))

	return response, nil
}
