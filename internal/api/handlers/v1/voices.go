package v1

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// voiceCatalog is a static catalog of known TTS voices per provider.
// It mirrors 9router's media-providers voice lists so clients can discover
// available voices without probing upstream providers. The `provider` query
// parameter filters the catalog (e.g. ?provider=openai,edge-tts,elevenlabs).
var voiceCatalog = map[string][]gin.H{
	"openai": {
		{ "id": "alloy", "name": "Alloy", "gender": "neutral" },
		{ "id": "ash", "name": "Ash", "gender": "neutral" },
		{ "id": "ballad", "name": "Ballad", "gender": "neutral" },
		{ "id": "coral", "name": "Coral", "gender": "neutral" },
		{ "id": "echo", "name": "Echo", "gender": "neutral" },
		{ "id": "fable", "name": "Fable", "gender": "neutral" },
		{ "id": "onyx", "name": "Onyx", "gender": "male" },
		{ "id": "nova", "name": "Nova", "gender": "female" },
		{ "id": "sage", "name": "Sage", "gender": "female" },
		{ "id": "shimmer", "name": "Shimmer", "gender": "female" },
		{ "id": "verse", "name": "Verse", "gender": "neutral" },
	},
	"edge-tts": {
		{ "id": "en-US-AriaNeural", "name": "Aria", "gender": "female", "locale": "en-US" },
		{ "id": "en-US-GuyNeural", "name": "Guy", "gender": "male", "locale": "en-US" },
		{ "id": "en-US-JennyNeural", "name": "Jenny", "gender": "female", "locale": "en-US" },
		{ "id": "en-US-MichelleNeural", "name": "Michelle", "gender": "female", "locale": "en-US" },
		{ "id": "en-US-RogerNeural", "name": "Roger", "gender": "male", "locale": "en-US" },
		{ "id": "en-GB-SoniaNeural", "name": "Sonia", "gender": "female", "locale": "en-GB" },
		{ "id": "en-GB-RyanNeural", "name": "Ryan", "gender": "male", "locale": "en-GB" },
		{ "id": "en-AU-NatashaNeural", "name": "Natasha", "gender": "female", "locale": "en-AU" },
		{ "id": "en-AU-WilliamNeural", "name": "William", "gender": "male", "locale": "en-AU" },
		{ "id": "id-ID-GadisNeural", "name": "Gadis", "gender": "female", "locale": "id-ID" },
		{ "id": "id-ID-ArdiNeural", "name": "Ardi", "gender": "male", "locale": "id-ID" },
		{ "id": "ja-JP-NanamiNeural", "name": "Nanami", "gender": "female", "locale": "ja-JP" },
		{ "id": "ja-JP-KeitaNeural", "name": "Keita", "gender": "male", "locale": "ja-JP" },
		{ "id": "ko-KR-SunHiNeural", "name": "SunHi", "gender": "female", "locale": "ko-KR" },
		{ "id": "ko-KR-InJoonNeural", "name": "InJoon", "gender": "male", "locale": "ko-KR" },
		{ "id": "zh-CN-XiaoxiaoNeural", "name": "Xiaoxiao", "gender": "female", "locale": "zh-CN" },
		{ "id": "zh-CN-YunxiNeural", "name": "Yunxi", "gender": "male", "locale": "zh-CN" },
		{ "id": "fr-FR-DeniseNeural", "name": "Denise", "gender": "female", "locale": "fr-FR" },
		{ "id": "de-DE-KatjaNeural", "name": "Katja", "gender": "female", "locale": "de-DE" },
		{ "id": "de-DE-ConradNeural", "name": "Conrad", "gender": "male", "locale": "de-DE" },
		{ "id": "es-ES-ElviraNeural", "name": "Elvira", "gender": "female", "locale": "es-ES" },
		{ "id": "pt-BR-FranciscaNeural", "name": "Francisca", "gender": "female", "locale": "pt-BR" },
		{ "id": "hi-IN-SwaraNeural", "name": "Swara", "gender": "female", "locale": "hi-IN" },
	},
	"elevenlabs": {
		{ "id": "21m00Tcm4TlvDq8ikWAM", "name": "Rachel", "gender": "female" },
		{ "id": "EXAVITQu4vr4xnSDxMaL", "name": "Sarah", "gender": "female" },
		{ "id": "yoZ06aMxZJJ28mfd3POQ", "name": "Sam", "gender": "male" },
		{ "id": "onwK4e9ZLuTAKqWW03F9", "name": "Domi", "gender": "female" },
		{ "id": "pNInz6obpgDQGcFmaJgB", "name": "Adam", "gender": "male" },
		{ "id": "VR6AewLTigWG4xSOukaG", "name": "Arnold", "gender": "male" },
		{ "id": "ThT5KcBeYPX3keUQqHPh", "name": "Bella", "gender": "female" },
	},
	"cartesia": {
		{ "id": "a0eabc41-6955-4d8a-8d13-1d6ed6d0d19e", "name": "Heart", "gender": "female" },
		{ "id": "95856005-0332-41b0-b935-2421b1c89b66", "name": "Cello", "gender": "male" },
	},
	"deepgram": {
		{ "id": "aura-asteria-en", "name": "Asteria", "gender": "female", "locale": "en" },
		{ "id": "aura-luna-en", "name": "Luna", "gender": "female", "locale": "en" },
		{ "id": "aura-stella-en", "name": "Stella", "gender": "female", "locale": "en" },
		{ "id": "aura-athena-en", "name": "Athena", "gender": "female", "locale": "en" },
		{ "id": "aura-hera-en", "name": "Hera", "gender": "female", "locale": "en" },
		{ "id": "aura-orion-en", "name": "Orion", "gender": "male", "locale": "en" },
		{ "id": "aura-arcus-en", "name": "Arcus", "gender": "male", "locale": "en" },
		{ "id": "aura-perseus-en", "name": "Perseus", "gender": "male", "locale": "en" },
		{ "id": "aura-angus-en", "name": "Angus", "gender": "male", "locale": "en" },
		{ "id": "aura-orpheus-en", "name": "Orpheus", "gender": "male", "locale": "en" },
		{ "id": "aura-helios-en", "name": "Helios", "gender": "male", "locale": "en" },
		{ "id": "aura-zeus-en", "name": "Zeus", "gender": "male", "locale": "en" },
	},
	"minimax": {
		{ "id": "male-qn-qingse", "name": "Qingse (Male)", "gender": "male", "locale": "zh-CN" },
		{ "id": "female-shaonv", "name": "Shaonv (Female)", "gender": "female", "locale": "zh-CN" },
		{ "id": "female-chengshu", "name": "Chengshu (Female)", "gender": "female", "locale": "zh-CN" },
		{ "id": "female-tianmei", "name": "Tianmei (Female)", "gender": "female", "locale": "zh-CN" },
		{ "id": "male-tianlong", "name": "Tianlong (Male)", "gender": "male", "locale": "zh-CN" },
	},
	"edge": {
		{ "id": "en-US-AriaNeural", "name": "Aria", "gender": "female", "locale": "en-US" },
		{ "id": "en-US-GuyNeural", "name": "Guy", "gender": "male", "locale": "en-US" },
	},
}

// Voices handles GET /v1/audio/voices — returns the known TTS voice catalog,
// optionally filtered by the `provider` query parameter.
func (h *Handler) Voices(c *gin.Context) {
	providerFilter := strings.ToLower(c.Query("provider"))
	var voices []gin.H
	if providerFilter != "" {
		for _, p := range strings.Split(providerFilter, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if list, ok := voiceCatalog[p]; ok {
				voices = append(voices, list...)
			}
		}
	} else {
		// Deterministic ordering: stable provider order, then insertion order.
		for _, p := range []string{"openai", "edge-tts", "edge", "elevenlabs", "cartesia", "deepgram", "minimax"} {
			voices = append(voices, voiceCatalog[p]...)
		}
	}
	if voices == nil {
		voices = []gin.H{}
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": voices})
}
