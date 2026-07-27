package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// cfClassifyClassifier handles text classification via Cloudflare's native
// /ai/run/{model} endpoint. It unwraps the "result" envelope and returns an
// OpenAI-compatible classification shape: {"object":"classification",
// "results":[{"label":"...","score":...},...]}.
func (e *CloudflareExecutor) cfClassifyClassifier(ctx context.Context, req *Request) (*Response, error) {
	modelName := gjson.GetBytes(req.Body, "model").String()
	text := gjson.GetBytes(req.Body, "text").String()
	if text == "" {
		return nil, &UpstreamError{
			StatusCode: http.StatusBadRequest,
			Body:       []byte(`{"error":{"message":"text is required for classification","type":"invalid_request_error"}}`),
			RawBody:    []byte(`{"error":{"message":"text is required for classification","type":"invalid_request_error"}}`),
		}
	}
	payload, _ := json.Marshal(map[string]any{"text": text})
	resp, err := e.cfNativeRun(ctx, req, modelName, payload)
	if err != nil {
		return nil, err
	}
	result := cfUnwrapResult(resp.Body)
	labels, err := cfClassificationLabels(result)
	if err != nil {
		labels = []map[string]any{{"result": json.RawMessage(result)}}
	}
	out := map[string]any{
		"object":  "classification",
		"model":   modelName,
		"results": labels,
	}
	outBody, _ := json.Marshal(out)
	return &Response{
		StatusCode: http.StatusOK,
		Headers:    resp.Headers,
		Body:       outBody,
	}, nil
}

// cfClassificationLabels parses common Cloudflare text-classification result
// shapes into a normalized label/score list.
func cfClassificationLabels(result []byte) ([]map[string]any, error) {
	var parsed struct {
		Result []map[string]any `json:"result"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(parsed.Result))
	for _, item := range parsed.Result {
		label, _ := item["label"].(string)
		var score float64
		switch v := item["score"].(type) {
		case float64:
			score = v
		case float32:
			score = float64(v)
		case json.Number:
			score, _ = v.Float64()
		}
		if label == "" {
			label, _ = item["name"].(string)
		}
		out = append(out, map[string]any{"label": label, "score": score})
	}
	return out, nil
}

// cfRerank handles rerank requests via Cloudflare's native /ai/run/{model}
// endpoint. The input body uses query + contexts; the upstream payload is
// translated to Cloudflare's shape. Response is normalized to a standard
// rerank result list.
func (e *CloudflareExecutor) cfRerank(ctx context.Context, req *Request) (*Response, error) {
	modelName := gjson.GetBytes(req.Body, "model").String()
	query := gjson.GetBytes(req.Body, "query").String()
	if query == "" {
		return nil, &UpstreamError{
			StatusCode: http.StatusBadRequest,
			Body:       []byte(`{"error":{"message":"query is required for rerank","type":"invalid_request_error"}}`),
			RawBody:    []byte(`{"error":{"message":"query is required for rerank","type":"invalid_request_error"}}`),
		}
	}
	contexts, err := parseStringArray(req.Body, "contexts")
	if err != nil || len(contexts) == 0 {
		return nil, &UpstreamError{
			StatusCode: http.StatusBadRequest,
			Body:       []byte(`{"error":{"message":"contexts array is required for rerank","type":"invalid_request_error"}}`),
			RawBody:    []byte(`{"error":{"message":"contexts array is required for rerank","type":"invalid_request_error"}}`),
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"query":    query,
		"contexts": contexts,
	})
	resp, err := e.cfNativeRun(ctx, req, modelName, payload)
	if err != nil {
		return nil, err
	}
	result := cfUnwrapResult(resp.Body)
	ranks, err := cfRerankResults(result, contexts)
	if err != nil {
		return nil, &UpstreamError{
			StatusCode: http.StatusBadGateway,
			Body:       []byte(`{"error":{"message":"failed to parse rerank response","type":"server_error"}}`),
			RawBody:    []byte(`{"error":{"message":"failed to parse rerank response","type":"server_error"}}`),
		}
	}
	out := map[string]any{
		"object": "list",
		"model":  modelName,
		"data":   ranks,
	}
	outBody, _ := json.Marshal(out)
	return &Response{
		StatusCode: http.StatusOK,
		Headers:    resp.Headers,
		Body:       outBody,
	}, nil
}

// cfRerankResults parses common Cloudflare rerank result shapes into a
// standardized result list with index, text, and score.
func cfRerankResults(result []byte, contexts []string) ([]map[string]any, error) {
	var parsed struct {
		Result []map[string]any `json:"result"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(parsed.Result))
	for _, item := range parsed.Result {
		idx := -1
		switch v := item["index"].(type) {
		case float64:
			idx = int(v)
		case int:
			idx = v
		case json.Number:
			idx, _ = strconv.Atoi(string(v))
		}
		var score float64
		switch v := item["score"].(type) {
		case float64:
			score = v
		case float32:
			score = float64(v)
		case json.Number:
			score, _ = v.Float64()
		}
		text := ""
		if idx >= 0 && idx < len(contexts) {
			text = contexts[idx]
		}
		if t, ok := item["text"].(string); ok && t != "" {
			text = t
		}
		out = append(out, map[string]any{
			"object":          "rerank",
			"index":           idx,
			"text":            text,
			"score":           score,
			"relevance_score": score,
		})
	}
	return out, nil
}

// cfImageClassification handles image classification and object detection via
// Cloudflare's native /ai/run/{model} endpoint. It accepts image bytes or a URL,
// converts to CF's native input, unwraps the result, and returns OpenAI-compatible
// labels (with optional bounding boxes for object detection).
func (e *CloudflareExecutor) cfImageClassification(ctx context.Context, req *Request) (*Response, error) {
	modelName := gjson.GetBytes(req.Body, "model").String()

	var payload map[string]any
	if image := gjson.GetBytes(req.Body, "image"); image.Exists() && image.Type == gjson.String {
		payload = map[string]any{"image": image.String()}
	}
	if payload == nil {
		if imageBytes, err := base64DecodeField(req.Body, "image"); err == nil && len(imageBytes) > 0 {
			payload = map[string]any{"image": base64.StdEncoding.EncodeToString(imageBytes)}
		}
	}
	if payload == nil {
		if imageURL := gjson.GetBytes(req.Body, "image_url").String(); imageURL != "" {
			payload = map[string]any{"image": imageURL}
		}
	}
	if payload == nil {
		return nil, &UpstreamError{
			StatusCode: http.StatusBadRequest,
			Body:       []byte(`{"error":{"message":"image or image_url is required","type":"invalid_request_error"}}`),
			RawBody:    []byte(`{"error":{"message":"image or image_url is required","type":"invalid_request_error"}}`),
		}
	}

	payloadBytes, _ := json.Marshal(payload)
	resp, err := e.cfNativeRun(ctx, req, modelName, payloadBytes)
	if err != nil {
		return nil, err
	}

	result := cfUnwrapResult(resp.Body)
	items, err := cfImageClassificationResults(result)
	if err != nil {
		items = []map[string]any{{"result": json.RawMessage(result)}}
	}
	out := map[string]any{
		"object": "list",
		"model":  modelName,
		"data":   items,
	}
	outBody, _ := json.Marshal(out)
	return &Response{
		StatusCode: http.StatusOK,
		Headers:    resp.Headers,
		Body:       outBody,
	}, nil
}

// cfImageClassificationResults parses common Cloudflare image-classification and
// object-detection result shapes into a normalized label/score/box list.
func cfImageClassificationResults(result []byte) ([]map[string]any, error) {
	var parsed struct {
		Result []map[string]any `json:"result"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(parsed.Result))
	for _, item := range parsed.Result {
		label, _ := item["label"].(string)
		if label == "" {
			label, _ = item["name"].(string)
		}
		var score float64
		switch v := item["score"].(type) {
		case float64:
			score = v
		case float32:
			score = float64(v)
		case json.Number:
			score, _ = v.Float64()
		}
		entry := map[string]any{
			"label": label,
			"score": score,
		}
		if box, ok := item["box"].(map[string]any); ok {
			entry["box"] = box
		}
		out = append(out, entry)
	}
	return out, nil
}

// detectCFTask inspects the request model/body and returns one of the supported
// Cloudflare native-run task kinds and true. If the request should fall through
// to the standard chat path, it returns "" and false.
func detectCFTask(model string, body []byte) (string, bool) {
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "distilbert-sst") || strings.Contains(lower, "-classification"):
		return "text-classification", true
	case strings.Contains(lower, "rerank"):
		return "rerank", true
	case strings.Contains(lower, "resnet-50") || strings.Contains(lower, "detr-resnet") || strings.Contains(lower, "-image-classification"):
		return "image-classification", true
	}
	return "", false
}

func parseStringArray(body []byte, path string) ([]string, error) {
	r := gjson.GetBytes(body, path)
	if !r.IsArray() {
		return nil, fmt.Errorf("%s is not an array", path)
	}
	var out []string
	for _, v := range r.Array() {
		out = append(out, v.String())
	}
	return out, nil
}

func base64DecodeField(body []byte, path string) ([]byte, error) {
	r := gjson.GetBytes(body, path)
	if r.Type != gjson.String {
		return nil, fmt.Errorf("field is not a string")
	}
	s := r.String()
	if idx := strings.Index(s, ","); idx != -1 {
		s = s[idx+1:]
	}
	return base64.StdEncoding.DecodeString(s)
}

// fetchImageBytes downloads an image from a URL and returns its bytes.
func fetchImageBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("image download failed: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// executePrivateImageDownload is a placeholder kept for potential future use.
var executePrivateImageDownload = fetchImageBytes
