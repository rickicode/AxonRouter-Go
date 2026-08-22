package executor

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestIsAntigravityImageModel(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"gemini-3-pro-image", true},
		{"gemini-3.1-flash-image", true},
		{"gemini-3-pro-image-widescreen", true},
		{"gemini-2.5-flash-image-preview", true},
		{"gemini-2.5-pro", false},
		{"gemini-3.1-pro", false},
		{"claude-sonnet-4-6", false},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			if got := isAntigravityImageModel(tc.model); got != tc.want {
				t.Errorf("isAntigravityImageModel(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

func TestStripAntigravityImageSuffix(t *testing.T) {
	cases := []struct {
		model           string
		wantModel       string
		wantAspectRatio string
	}{
		{"gemini-3-pro-image-widescreen", "gemini-3-pro-image", "widescreen"},
		{"gemini-3-pro-image-portrait", "gemini-3-pro-image", "portrait"},
		{"GEMINI-3-PRO-IMAGE-PORTRAIT", "GEMINI-3-PRO-IMAGE", "portrait"},
		{"gemini-3-pro-image-square", "gemini-3-pro-image", "square"},
		{"gemini-3-pro-image-landscape", "gemini-3-pro-image", "landscape"},
		{"gemini-3-pro-image", "gemini-3-pro-image", ""},
		{"gemini-3-pro-image-custom", "gemini-3-pro-image", "custom"},
		{"gemini-3-pro", "gemini-3-pro", ""},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			gotModel, gotAspect := stripAntigravityImageSuffix(tc.model)
			if gotModel != tc.wantModel || gotAspect != tc.wantAspectRatio {
				t.Errorf("stripAntigravityImageSuffix(%q) = (%q, %q), want (%q, %q)", tc.model, gotModel, gotAspect, tc.wantModel, tc.wantAspectRatio)
			}
		})
	}
}

func TestBuildAntigravityImageEnvelope(t *testing.T) {
	envelope := []byte(`{
		"project":"proj-123",
		"model":"gemini-3-pro-image-widescreen",
		"requestType":"agent",
		"request":{
			"contents":[{"role":"user","parts":[{"text":"a cat in a spacesuit"}]}],
			"generationConfig":{
				"temperature":0.9,
				"candidateCount":2,
				"imageConfig":{"aspectRatio":"16:9","imageSize":"1024x576"}
			},
			"safetySettings":[{"category":"HARM_CATEGORY_HARASSMENT","threshold":"OFF"}],
			"toolConfig":{"functionCallingConfig":{"mode":"VALIDATED"}}
		}
	}`)

	out, err := buildAntigravityImageEnvelope(envelope, "gemini-3-pro-image")
	if err != nil {
		t.Fatalf("buildAntigravityImageEnvelope failed: %v", err)
	}

	root := gjson.ParseBytes(out)
	if root.Get("model").String() != "gemini-3-pro-image" {
		t.Errorf("expected model gemini-3-pro-image, got %s", root.Get("model").String())
	}
	if root.Get("requestType").String() != "image_gen" {
		t.Errorf("expected requestType image_gen, got %s", root.Get("requestType").String())
	}
	if root.Get("request.generationConfig").Exists() {
		t.Error("expected request.generationConfig to be removed")
	}
	if root.Get("request.toolConfig").Exists() {
		t.Error("expected request.toolConfig to be removed")
	}
	if root.Get("request.safetySettings").Exists() {
		t.Error("expected request.safetySettings to be removed")
	}
	if root.Get("request.parameters.aspectRatio").String() != "16:9" {
		t.Errorf("expected parameters.aspectRatio 16:9, got %s", root.Get("request.parameters.aspectRatio").String())
	}
	if root.Get("request.parameters.imageSize").String() != "1024x576" {
		t.Errorf("expected parameters.imageSize 1024x576, got %s", root.Get("request.parameters.imageSize").String())
	}
	if root.Get("request.parameters.numberOfImages").Int() != 2 {
		t.Errorf("expected parameters.numberOfImages 2, got %d", root.Get("request.parameters.numberOfImages").Int())
	}
	if root.Get("request.contents.0.parts.0.text").String() != "a cat in a spacesuit" {
		t.Errorf("expected prompt preserved, got %s", root.Get("request.contents.0.parts.0.text").String())
	}
}

func TestMapAntigravityImageResponse(t *testing.T) {
	raw := []byte(`{
		"predictions":[
			{"bytesBase64Encoded":"aW1hZ2Ux"},
			{"content":{"parts":[{"inlineData":{"mimeType":"image/jpeg","data":"aW1hZ2Uy"}}]}}
		],
		"modelVersion":"gemini-3-pro-image-001"
	}`)

	out := mapAntigravityImageResponse(raw)
	root := gjson.ParseBytes(out)

	parts := root.Get("response.candidates.0.content.parts").Array()
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Get("inlineData.data").String() != "aW1hZ2Ux" {
		t.Errorf("expected first inline data aW1hZ2Ux, got %s", parts[0].Get("inlineData.data").String())
	}
	if parts[0].Get("inlineData.mimeType").String() != "image/png" {
		t.Errorf("expected first mime image/png, got %s", parts[0].Get("inlineData.mimeType").String())
	}
	if parts[1].Get("inlineData.data").String() != "aW1hZ2Uy" {
		t.Errorf("expected second inline data aW1hZ2Uy, got %s", parts[1].Get("inlineData.data").String())
	}
	if root.Get("response.modelVersion").String() != "gemini-3-pro-image-001" {
		t.Errorf("expected modelVersion preserved, got %s", root.Get("response.modelVersion").String())
	}
}

func TestMapAntigravityImageResponse_PassthroughWhenAlreadyWrapped(t *testing.T) {
	raw := []byte(`{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}]}}`)
	out := mapAntigravityImageResponse(raw)
	if string(out) != string(raw) {
		t.Errorf("expected passthrough, got %s", string(out))
	}
}

func TestAntigravityImageURL(t *testing.T) {
	url := antigravityImageURL("")
	if url != "https://cloudcode-pa.googleapis.com/v1internal:generateImage" {
		t.Errorf("expected default image url, got %q", url)
	}
	url = antigravityImageURL("https://cloudcode-pa.googleapis.com/v1internal:generateContent")
	if url != "https://cloudcode-pa.googleapis.com/v1internal:generateImage" {
		t.Errorf("expected generateContent replaced with generateImage, got %q", url)
	}
	url = antigravityImageURL("https://daily-cloudcode-pa.googleapis.com/custom/path")
	if url != "https://daily-cloudcode-pa.googleapis.com/custom/path/v1internal:generateImage" {
		t.Errorf("expected custom base path rewritten to generateImage, got %q", url)
	}
}
