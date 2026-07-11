package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

func main() {
	logging.Init("text")
	accountID := os.Getenv("CF_ACCOUNT_ID")
	apiToken := os.Getenv("CF_API_TOKEN")
	if accountID == "" || apiToken == "" {
		fmt.Println("set CF_ACCOUNT_ID and CF_API_TOKEN")
		os.Exit(1)
	}

	base := executor.NewBaseExecutor()
	base.Timeout = 10 * time.Second
	exec := executor.NewOpenAIExecutor(base)

	body := []byte(`{"model":"@cf/meta/llama-3.2-1b-instruct","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`)
	req := &executor.Request{
		Model:       "@cf/meta/llama-3.2-1b-instruct",
		Body:        body,
		Stream:      false,
		APIKey:      apiToken,
		BaseURL:     "https://api.cloudflare.com/client/v4/accounts/{accountId}/ai/v1/chat/completions",
		Provider:    "cf",
		ProviderSpecificData: map[string]string{"accountId": accountID},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := exec.Execute(ctx, req)
	fmt.Printf("elapsed=%v\n", time.Since(start))
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("status=%d body=%s\n", resp.StatusCode, string(resp.Body))
}
