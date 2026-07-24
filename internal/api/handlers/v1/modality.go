package v1

import (
	"strconv"

	"github.com/rickicode/AxonRouter-Go/internal/executor"
)

// ttsCharsPerMinute is a rough heuristic for English text-to-speech duration.
// It is only used for cost estimation, not billing precision.
const ttsCharsPerMinute int64 = 1000

// quantityForModality returns the modality-specific consumption unit to use for
// cost estimation. For image/video it reads "n" from the request body (default 1).
// For TTS it estimates minutes from the "input" text length. For STT it returns
// 1 minute because the uploaded file duration is not parsed here.
func quantityForModality(modality string, body []byte) int64 {
	switch modality {
	case "image", "video":
		nStr := executor.JSONGet(body, "n")
		if n, err := strconv.ParseInt(nStr, 10, 64); err == nil && n > 0 {
			return n
		}
		return 1
	case "audio", "tts":
		text := executor.JSONGet(body, "input")
		if text == "" {
			return 1
		}
		minutes := int64(len(text)) / ttsCharsPerMinute
		if minutes == 0 {
			return 1
		}
		return minutes
	case "stt":
		// Uploaded audio duration is unknown without decoding; default to 1 minute.
		return 1
	default:
		return 0
	}
}
