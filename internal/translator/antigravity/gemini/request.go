package gemini

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertAntigravityRequestToGemini converts an Antigravity request to Gemini generateContent format.
// Antigravity uses a Gemini-like format but nested under "request" key.
func convertAntigravityRequestToGemini(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := []byte(`{"contents":[]}`)
	out, _ = sjson.SetBytes(out, "model", modelName)

	// Generation config
	if genConfig := root.Get("request.generationConfig"); genConfig.Exists() {
		if maxTokens := genConfig.Get("maxOutputTokens"); maxTokens.Exists() {
			out, _ = sjson.SetBytes(out, "generationConfig.maxOutputTokens", maxTokens.Int())
		}
		if temp := genConfig.Get("temperature"); temp.Exists() {
			out, _ = sjson.SetBytes(out, "generationConfig.temperature", temp.Float())
		}
		if topP := genConfig.Get("topP"); topP.Exists() {
			out, _ = sjson.SetBytes(out, "generationConfig.topP", topP.Float())
		}
		if topK := genConfig.Get("topK"); topK.Exists() {
			out, _ = sjson.SetBytes(out, "generationConfig.topK", topK.Int())
		}
		if stopSeqs := genConfig.Get("stopSequences"); stopSeqs.Exists() && stopSeqs.IsArray() {
			var stops []string
			stopSeqs.ForEach(func(_, v gjson.Result) bool {
				stops = append(stops, v.String())
				return true
			})
			if len(stops) > 0 {
				out, _ = sjson.SetBytes(out, "generationConfig.stopSequences", stops)
			}
		}
	}

	// System instruction
	if sysInst := root.Get("request.systemInstruction"); sysInst.Exists() {
		if parts := sysInst.Get("parts"); parts.Exists() && parts.IsArray() {
			var textParts []map[string]interface{}
			parts.ForEach(func(_, part gjson.Result) bool {
				if text := part.Get("text"); text.Exists() {
					textParts = append(textParts, map[string]interface{}{
						"text": text.String(),
					})
				}
				return true
			})
			if len(textParts) > 0 {
				out, _ = sjson.SetBytes(out, "systemInstruction.role", "user")
				b, _ := json.Marshal(textParts)
				out, _ = sjson.SetRawBytes(out, "systemInstruction.parts", b)
			}
		}
	}

	// Contents
	if contents := root.Get("request.contents"); contents.Exists() && contents.IsArray() {
		contents.ForEach(func(_, content gjson.Result) bool {
			node := []byte(`{"role":"","parts":[]}`)
			node, _ = sjson.SetBytes(node, "role", content.Get("role").String())

			if parts := content.Get("parts"); parts.Exists() && parts.IsArray() {
				p := 0
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						node, _ = sjson.SetBytes(node, fmt.Sprintf("parts.%d.text", p), text.String())
						p++
					}
					if fc := part.Get("functionCall"); fc.Exists() {
						node, _ = sjson.SetRawBytes(node, fmt.Sprintf("parts.%d.functionCall", p), []byte(fc.Raw))
						p++
					}
					if fr := part.Get("functionResponse"); fr.Exists() {
						node, _ = sjson.SetRawBytes(node, fmt.Sprintf("parts.%d.functionResponse", p), []byte(fr.Raw))
						p++
					}
					return true
				})
			}

			out, _ = sjson.SetRawBytes(out, "contents.-1", node)
			return true
		})
	}

	// Tools
	if tools := root.Get("request.tools"); tools.Exists() && tools.IsArray() {
		var functionDeclarations []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			if decls := tool.Get("functionDeclarations"); decls.Exists() && decls.IsArray() {
				decls.ForEach(func(_, decl gjson.Result) bool {
					fn := map[string]interface{}{
						"name":        decl.Get("name").String(),
						"description": decl.Get("description").String(),
					}
					if params := decl.Get("parameters"); params.Exists() {
						var p interface{}
						json.Unmarshal([]byte(params.Raw), &p)
						fn["parameters"] = p
					}
					functionDeclarations = append(functionDeclarations, fn)
					return true
				})
			}
			return true
		})
		if len(functionDeclarations) > 0 {
			b, _ := json.Marshal(functionDeclarations)
			out, _ = sjson.SetRawBytes(out, "tools.0.functionDeclarations", b)
		}
	}

	return out
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
