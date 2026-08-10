package common

import (
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// CopyCacheControlToMap copies a Claude-compatible cache_control object from src onto dst.
// It returns true when a cache_control object was actually copied. dst is modified in place.
func CopyCacheControlToMap(dst map[string]any, src gjson.Result) bool {
	cc := src.Get("cache_control")
	if !cc.Exists() || cc.Type == gjson.Null || !cc.IsObject() {
		return false
	}
	dst["cache_control"] = cc.Value()
	return true
}

// AttachMessageCacheControlToMap applies message-level cache_control from src onto the last
// content block of msg. Part-level cache_control wins when the last block already has one.
// String content is promoted to a single-element array so it can carry cache_control.
// It returns true when a cache_control object was actually applied. msg is modified in place.
func AttachMessageCacheControlToMap(msg map[string]any, src gjson.Result) bool {
	cc := src.Get("cache_control")
	if !cc.Exists() || cc.Type == gjson.Null || !cc.IsObject() {
		return false
	}

	content := msg["content"]
	switch v := content.(type) {
	case []map[string]any:
		if len(v) == 0 {
			return false
		}
		lastIdx := len(v) - 1
		if _, ok := v[lastIdx]["cache_control"]; ok {
			return false
		}
		v[lastIdx]["cache_control"] = cc.Value()
	case string:
		msg["content"] = []map[string]any{
			{"type": "text", "text": v, "cache_control": cc.Value()},
		}
	}

	return true
}

// AttachCacheControl copies a Claude-compatible cache_control object from src onto dst.
// Returns dst unchanged when cache_control is missing or not an object.
func AttachCacheControl(dst []byte, src gjson.Result) []byte {
	cc := src.Get("cache_control")
	if !cc.Exists() || cc.Type == gjson.Null || !cc.IsObject() {
		return dst
	}
	out, err := sjson.SetRawBytes(dst, "cache_control", []byte(cc.Raw))
	if err != nil {
		return dst
	}
	return out
}

// AttachMessageCacheControl applies message-level cache_control onto the last content block.
// Part-level cache_control wins when the last block already has one.
// String content is promoted to a content array so Claude can accept cache_control.
func AttachMessageCacheControl(msg []byte, src gjson.Result) []byte {
	cc := src.Get("cache_control")
	if !cc.Exists() || cc.Type == gjson.Null || !cc.IsObject() {
		return msg
	}

	content := gjson.GetBytes(msg, "content")
	if content.IsArray() {
		arr := content.Array()
		if len(arr) == 0 {
			return msg
		}
		lastIdx := len(arr) - 1
		if arr[lastIdx].Get("cache_control").Exists() {
			return msg
		}
		path := fmt.Sprintf("content.%d.cache_control", lastIdx)
		out, err := sjson.SetRawBytes(msg, path, []byte(cc.Raw))
		if err != nil {
			return msg
		}
		return out
	}

	if content.Type != gjson.String {
		return msg
	}

	textPart := []byte(`{"type":"text","text":""}`)
	textPart, _ = sjson.SetBytes(textPart, "text", content.String())
	textPart, errSet := sjson.SetRawBytes(textPart, "cache_control", []byte(cc.Raw))
	if errSet != nil {
		return msg
	}
	out, err := sjson.SetRawBytes(msg, "content", []byte("[]"))
	if err != nil {
		return msg
	}
	out, _ = sjson.SetRawBytes(out, "content.-1", textPart)
	return out
}
