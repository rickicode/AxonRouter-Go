package openai

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/translator/antigravity"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const antigravityFunctionThoughtSignature = "skip_thought_signature_validator"

var functionNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_.:-]`)

// MimeTypes maps file extensions to their MIME types.
var MimeTypes = map[string]string{
	"png":  "image/png",
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"webp": "image/webp",
	"gif":  "image/gif",
	"pdf":  "application/pdf",
	"txt":  "text/plain",
	"csv":  "text/csv",
	"html": "text/html",
	"mp3":  "audio/mpeg",
	"wav":  "audio/wav",
	"ogg":  "audio/ogg",
	"flac": "audio/flac",
	"aac":  "audio/aac",
	"webm": "audio/webm",
}

// SanitizeFunctionName ensures a function name matches the requirements for Gemini/Vertex AI.
func SanitizeFunctionName(name string) string {
	if name == "" {
		return ""
	}
	sanitized := functionNameSanitizer.ReplaceAllString(name, "_")
	if len(sanitized) > 0 {
		first := sanitized[0]
		if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {
			if len(sanitized) >= 64 {
				sanitized = sanitized[:63]
			}
			sanitized = "_" + sanitized
		}
	} else {
		sanitized = "_"
	}
	if len(sanitized) > 64 {
		sanitized = sanitized[:64]
	}
	return sanitized
}

// RenameKey renames a key in a JSON string.
func RenameKey(jsonStr, oldKeyPath, newKeyPath string) (string, error) {
	value := gjson.Get(jsonStr, oldKeyPath)
	if !value.Exists() {
		return "", fmt.Errorf("old key '%s' does not exist", oldKeyPath)
	}
	interimJSON, errSet := sjson.SetRawBytes([]byte(jsonStr), newKeyPath, []byte(value.Raw))
	if errSet != nil {
		return "", fmt.Errorf("failed to set new key '%s': %w", newKeyPath, errSet)
	}
	finalJSON, errDelete := sjson.DeleteBytes(interimJSON, oldKeyPath)
	if errDelete != nil {
		return "", fmt.Errorf("failed to delete old key '%s': %w", oldKeyPath, errDelete)
	}
	return string(finalJSON), nil
}

// DefaultSafetySettings returns the default safety settings.
func DefaultSafetySettings() []map[string]string {
	return []map[string]string{
		{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "OFF"},
		{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "OFF"},
		{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "OFF"},
		{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "OFF"},
		{"category": "HARM_CATEGORY_CIVIC_INTEGRITY", "threshold": "BLOCK_NONE"},
	}
}

// AttachDefaultSafetySettings ensures safety settings are attached.
func AttachDefaultSafetySettings(rawJSON []byte, path string) []byte {
	if gjson.GetBytes(rawJSON, path).Exists() {
		return rawJSON
	}
	out, err := sjson.SetBytes(rawJSON, path, DefaultSafetySettings())
	if err != nil {
		return rawJSON
	}
	return out
}

// convertOpenAIRequestToAntigravity converts OpenAI Chat Completions requests to Antigravity format.
func convertOpenAIRequestToAntigravity(modelName string, inputRawJSON []byte, _ bool) []byte {
	rawJSON := inputRawJSON
	out := []byte(`{"project":"","request":{"contents":[]},"model":"gemini-2.5-pro"}`)
	out, _ = sjson.SetBytes(out, "model", modelName)

	if genConfig := gjson.GetBytes(rawJSON, "generationConfig"); genConfig.Exists() {
		out, _ = sjson.SetRawBytes(out, "request.generationConfig", []byte(genConfig.Raw))
	}

	re := gjson.GetBytes(rawJSON, "reasoning_effort")
	if re.Exists() {
		effort := strings.ToLower(strings.TrimSpace(re.String()))
		if effort != "" {
			thinkingPath := "request.generationConfig.thinkingConfig"
			if effort == "auto" {
				out, _ = sjson.SetBytes(out, thinkingPath+".thinkingBudget", -1)
				out, _ = sjson.SetBytes(out, thinkingPath+".includeThoughts", true)
			} else {
				out, _ = sjson.SetBytes(out, thinkingPath+".thinkingLevel", effort)
				out, _ = sjson.SetBytes(out, thinkingPath+".includeThoughts", effort != "none")
			}
		}
	}

	if tr := gjson.GetBytes(rawJSON, "temperature"); tr.Exists() && tr.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "request.generationConfig.temperature", tr.Num)
	}
	if tpr := gjson.GetBytes(rawJSON, "top_p"); tpr.Exists() && tpr.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "request.generationConfig.topP", tpr.Num)
	}
	if tkr := gjson.GetBytes(rawJSON, "top_k"); tkr.Exists() && tkr.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "request.generationConfig.topK", tkr.Num)
	}
	if maxTok := gjson.GetBytes(rawJSON, "max_tokens"); maxTok.Exists() && maxTok.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "request.generationConfig.maxOutputTokens", maxTok.Num)
	}

	if n := gjson.GetBytes(rawJSON, "n"); n.Exists() && n.Type == gjson.Number {
		if val := n.Int(); val > 1 {
			out, _ = sjson.SetBytes(out, "request.generationConfig.candidateCount", val)
		}
	}

	if mods := gjson.GetBytes(rawJSON, "modalities"); mods.Exists() && mods.IsArray() {
		var responseMods []string
		for _, m := range mods.Array() {
			switch strings.ToLower(m.String()) {
			case "text":
				responseMods = append(responseMods, "TEXT")
			case "image":
				responseMods = append(responseMods, "IMAGE")
			}
		}
		if len(responseMods) > 0 {
			out, _ = sjson.SetBytes(out, "request.generationConfig.responseModalities", responseMods)
		}
	}

	if imgCfg := gjson.GetBytes(rawJSON, "image_config"); imgCfg.Exists() && imgCfg.IsObject() {
		if ar := imgCfg.Get("aspect_ratio"); ar.Exists() && ar.Type == gjson.String {
			out, _ = sjson.SetBytes(out, "request.generationConfig.imageConfig.aspectRatio", ar.Str)
		}
		if size := imgCfg.Get("image_size"); size.Exists() && size.Type == gjson.String {
			out, _ = sjson.SetBytes(out, "request.generationConfig.imageConfig.imageSize", size.Str)
		}
	}

	messages := gjson.GetBytes(rawJSON, "messages")
	if messages.IsArray() {
		arr := messages.Array()
		tcID2Name := map[string]string{}
		for i := 0; i < len(arr); i++ {
			m := arr[i]
			if m.Get("role").String() == "assistant" {
				tcs := m.Get("tool_calls")
				if tcs.IsArray() {
					for _, tc := range tcs.Array() {
						if tc.Get("type").String() == "function" {
							id := tc.Get("id").String()
							name := tc.Get("function.name").String()
							if id != "" && name != "" {
								tcID2Name[id] = name
							}
						}
					}
				}
			}
		}

		toolResponses := map[string]string{}
		for i := 0; i < len(arr); i++ {
			m := arr[i]
			role := m.Get("role").String()
			if role == "tool" {
				toolCallID := m.Get("tool_call_id").String()
				if toolCallID != "" {
					c := m.Get("content")
					toolResponses[toolCallID] = c.Raw
				}
			}
		}

		systemPartIndex := 0
		for i := 0; i < len(arr); i++ {
			m := arr[i]
			role := m.Get("role").String()
			content := m.Get("content")

			if (role == "system" || role == "developer") && len(arr) > 1 {
				if content.Type == gjson.String {
					out, _ = sjson.SetBytes(out, "request.systemInstruction.role", "user")
					out, _ = sjson.SetBytes(out, fmt.Sprintf("request.systemInstruction.parts.%d.text", systemPartIndex), content.String())
					systemPartIndex++
				} else if content.IsObject() && content.Get("type").String() == "text" {
					out, _ = sjson.SetBytes(out, "request.systemInstruction.role", "user")
					out, _ = sjson.SetBytes(out, fmt.Sprintf("request.systemInstruction.parts.%d.text", systemPartIndex), content.Get("text").String())
					systemPartIndex++
				} else if content.IsArray() {
					contents := content.Array()
					if len(contents) > 0 {
						out, _ = sjson.SetBytes(out, "request.systemInstruction.role", "user")
						for j := 0; j < len(contents); j++ {
							out, _ = sjson.SetBytes(out, fmt.Sprintf("request.systemInstruction.parts.%d.text", systemPartIndex), contents[j].Get("text").String())
							systemPartIndex++
						}
					}
				}
			} else if role == "user" || ((role == "system" || role == "developer") && len(arr) == 1) {
				node := []byte(`{"role":"user","parts":[]}`)
				if content.Type == gjson.String {
					node, _ = sjson.SetBytes(node, "parts.0.text", content.String())
				} else if content.IsArray() {
					items := content.Array()
					p := 0
					for _, item := range items {
						switch item.Get("type").String() {
						case "text":
							text := item.Get("text").String()
							if text != "" {
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".text", text)
								p++
							}
						case "image_url":
							imageURL := item.Get("image_url.url").String()
							if len(imageURL) > 5 {
								pieces := strings.SplitN(imageURL[5:], ";", 2)
								if len(pieces) == 2 && len(pieces[1]) > 7 {
									mime := pieces[0]
									data := pieces[1][7:]
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.mimeType", mime)
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.data", data)
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".thoughtSignature", antigravityFunctionThoughtSignature)
									p++
								}
							}
						case "file":
							filename := item.Get("file.filename").String()
							fileData := item.Get("file.file_data").String()
							ext := ""
							if sp := strings.Split(filename, "."); len(sp) > 1 {
								ext = sp[len(sp)-1]
							}
							if mimeType, ok := MimeTypes[ext]; ok {
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.mimeType", mimeType)
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.data", fileData)
								p++
							}
						case "input_audio":
							audioData := item.Get("input_audio.data").String()
							audioFormat := item.Get("input_audio.format").String()
							if audioData != "" {
								audioMimeMap := map[string]string{
									"mp3":       "audio/mpeg",
									"wav":       "audio/wav",
									"ogg":       "audio/ogg",
									"flac":      "audio/flac",
									"aac":       "audio/aac",
									"webm":      "audio/webm",
									"pcm16":     "audio/pcm",
									"g711_ulaw": "audio/basic",
									"g711_alaw": "audio/basic",
								}
								mimeType := "audio/wav"
								if audioFormat != "" {
									if mapped, ok := audioMimeMap[audioFormat]; ok {
										mimeType = mapped
									} else {
										mimeType = "audio/" + audioFormat
									}
								}
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.mimeType", mimeType)
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.data", audioData)
								p++
							}
						}
					}
				}
				out, _ = sjson.SetRawBytes(out, "request.contents.-1", node)
			} else if role == "assistant" {
				node := []byte(`{"role":"model","parts":[]}`)
				p := 0
				if content.Type == gjson.String && content.String() != "" {
					node, _ = sjson.SetBytes(node, "parts.-1.text", content.String())
					p++
				} else if content.IsArray() {
					for _, item := range content.Array() {
						switch item.Get("type").String() {
						case "text":
							text := item.Get("text").String()
							if text != "" {
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".text", text)
								p++
							}
						case "image_url":
							imageURL := item.Get("image_url.url").String()
							if len(imageURL) > 5 {
								pieces := strings.SplitN(imageURL[5:], ";", 2)
								if len(pieces) == 2 && len(pieces[1]) > 7 {
									mime := pieces[0]
									data := pieces[1][7:]
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.mimeType", mime)
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.data", data)
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".thoughtSignature", antigravityFunctionThoughtSignature)
									p++
								}
							}
						}
					}
				}

				tcs := m.Get("tool_calls")
				if tcs.IsArray() {
					fIDs := make([]string, 0)
					for _, tc := range tcs.Array() {
						if tc.Get("type").String() != "function" {
							continue
						}
						fid := tc.Get("id").String()
						fname := antigravity.CloakName(SanitizeFunctionName(tc.Get("function.name").String()))
						fargs := tc.Get("function.arguments").String()
						node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".functionCall.id", fid)
						node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".functionCall.name", fname)
						if gjson.Valid(fargs) {
							node, _ = sjson.SetRawBytes(node, "parts."+itoa(p)+".functionCall.args", []byte(fargs))
						} else {
							node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".functionCall.args.params", []byte(fargs))
						}
						sig := cache.GetCachedSignature("", fargs)
						if sig == "" {
							sig = antigravityFunctionThoughtSignature
						}
						node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".thoughtSignature", sig)
						p++
						if fid != "" {
							fIDs = append(fIDs, fid)
						}
					}
					out, _ = sjson.SetRawBytes(out, "request.contents.-1", node)

					toolNode := []byte(`{"role":"user","parts":[]}`)
					pp := 0
					for _, fid := range fIDs {
						if name, ok := tcID2Name[fid]; ok {
							toolNode, _ = sjson.SetBytes(toolNode, "parts."+itoa(pp)+".functionResponse.id", fid)
							toolNode, _ = sjson.SetBytes(toolNode, "parts."+itoa(pp)+".functionResponse.name", antigravity.CloakName(SanitizeFunctionName(name)))
							resp := toolResponses[fid]
							if resp == "" {
								resp = "{}"
							}
							if resp != "null" {
								parsed := gjson.Parse(resp)
								if parsed.Type == gjson.JSON {
									toolNode, _ = sjson.SetRawBytes(toolNode, "parts."+itoa(pp)+".functionResponse.response.result", []byte(parsed.Raw))
								} else {
									toolNode, _ = sjson.SetBytes(toolNode, "parts."+itoa(pp)+".functionResponse.response.result", resp)
								}
							}
							pp++
						}
					}
					if pp > 0 {
						out, _ = sjson.SetRawBytes(out, "request.contents.-1", toolNode)
					}
				} else {
					out, _ = sjson.SetRawBytes(out, "request.contents.-1", node)
				}
			}
		}
	}

	tools := gjson.GetBytes(rawJSON, "tools")
	if tools.IsArray() && len(tools.Array()) > 0 {
		functionToolNode := []byte(`{}`)
		hasFunction := false
		googleSearchNodes := make([][]byte, 0)
		codeExecutionNodes := make([][]byte, 0)
		urlContextNodes := make([][]byte, 0)
		for _, t := range tools.Array() {
			if t.Get("type").String() == "function" {
				fn := t.Get("function")
				if fn.Exists() && fn.IsObject() {
					fnRaw := fn.Raw
					// Strip enumDescriptions (Antigravity rejects with HTTP 400)
					if params := fn.Get("parameters"); params.Exists() {
						var parsed map[string]any
						if json.Unmarshal([]byte(params.Raw), &parsed) == nil {
							cleaned := antigravity.StripEnumDescriptions(parsed)
							if b, err := json.Marshal(cleaned); err == nil {
								fnRawBytes, _ := sjson.SetRawBytes([]byte(fnRaw), "parameters", b)
								fnRaw = string(fnRawBytes)
							}
						}
					}
					if fn.Get("parameters").Exists() {
						renamed, errRename := RenameKey(fnRaw, "parameters", "parametersJsonSchema")
						if errRename != nil {
							var errSet error
							fnRawBytes, errSet := sjson.SetBytes([]byte(fnRaw), "parametersJsonSchema.type", "object")
							if errSet != nil {
								continue
							}
							fnRaw = string(fnRawBytes)
							fnRawBytes, errSet = sjson.SetRawBytes([]byte(fnRaw), "parametersJsonSchema.properties", []byte(`{}`))
							if errSet != nil {
								continue
							}
							fnRaw = string(fnRawBytes)
						} else {
							fnRaw = renamed
						}
					} else {
						var errSet error
						fnRawBytes, errSet := sjson.SetBytes([]byte(fnRaw), "parametersJsonSchema.type", "object")
						if errSet != nil {
							continue
						}
						fnRaw = string(fnRawBytes)
						fnRawBytes, errSet = sjson.SetRawBytes([]byte(fnRaw), "parametersJsonSchema.properties", []byte(`{}`))
						if errSet != nil {
							continue
						}
						fnRaw = string(fnRawBytes)
					}
					fnRawBytes := []byte(fnRaw)
					fnRawBytes, _ = sjson.SetBytes(fnRawBytes, "name", antigravity.CloakName(SanitizeFunctionName(fn.Get("name").String())))
					fnRaw, _ = sjson.Delete(string(fnRawBytes), "strict")
					if !hasFunction {
						functionToolNode, _ = sjson.SetRawBytes(functionToolNode, "functionDeclarations", []byte("[]"))
					}
					tmp, errSet := sjson.SetRawBytes(functionToolNode, "functionDeclarations.-1", []byte(fnRaw))
					if errSet != nil {
						continue
					}
					functionToolNode = tmp
					hasFunction = true
				}
			}
			if gs := t.Get("google_search"); gs.Exists() {
				googleToolNode := []byte(`{}`)
				var errSet error
				googleToolNode, errSet = sjson.SetRawBytes(googleToolNode, "googleSearch", []byte(gs.Raw))
				if errSet != nil {
					continue
				}
				googleSearchNodes = append(googleSearchNodes, googleToolNode)
			}
			if ce := t.Get("code_execution"); ce.Exists() {
				codeToolNode := []byte(`{}`)
				var errSet error
				codeToolNode, errSet = sjson.SetRawBytes(codeToolNode, "codeExecution", []byte(ce.Raw))
				if errSet != nil {
					continue
				}
				codeExecutionNodes = append(codeExecutionNodes, codeToolNode)
			}
			if uc := t.Get("url_context"); uc.Exists() {
				urlToolNode := []byte(`{}`)
				var errSet error
				urlToolNode, errSet = sjson.SetRawBytes(urlToolNode, "urlContext", []byte(uc.Raw))
				if errSet != nil {
					continue
				}
				urlContextNodes = append(urlContextNodes, urlToolNode)
			}
		}
		if hasFunction || len(googleSearchNodes) > 0 || len(codeExecutionNodes) > 0 || len(urlContextNodes) > 0 {
			toolsNode := []byte("[]")
			if hasFunction {
				// Track declared tool names to avoid duplicates
				declaredNames := make(map[string]bool)
				if decls := gjson.GetBytes(functionToolNode, "functionDeclarations"); decls.Exists() {
					for _, d := range decls.Array() {
						if n := d.Get("name").String(); n != "" {
							declaredNames[n] = true
						}
					}
				}
				// Inject decoy tools into functionDeclarations (skip if already declared)
				for _, decoyName := range antigravity.AGDecoyToolNames {
					if declaredNames[decoyName] {
						continue
					}
					decoyDecl := fmt.Sprintf(`{"name":%q,"description":"This tool is currently unavailable.","parametersJsonSchema":{"type":"object","properties":{}}}`, decoyName)
					functionToolNode, _ = sjson.SetRawBytes(functionToolNode, "functionDeclarations.-1", []byte(decoyDecl))
				}
				toolsNode, _ = sjson.SetRawBytes(toolsNode, "-1", functionToolNode)
			}
			for _, googleNode := range googleSearchNodes {
				toolsNode, _ = sjson.SetRawBytes(toolsNode, "-1", googleNode)
			}
			for _, codeNode := range codeExecutionNodes {
				toolsNode, _ = sjson.SetRawBytes(toolsNode, "-1", codeNode)
			}
			for _, urlNode := range urlContextNodes {
				toolsNode, _ = sjson.SetRawBytes(toolsNode, "-1", urlNode)
			}
			out, _ = sjson.SetRawBytes(out, "request.tools", toolsNode)
		}
	}

	return AttachDefaultSafetySettings(out, "request.safetySettings")
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
