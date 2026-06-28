package connstate

// Error patterns for classification. Hardcoded for fast matching.
// These cover the most common error messages from OpenAI, Claude, Gemini, and other providers.

var RateLimitPatterns = []string{
	"rate limit",
	"rate_limit",
	"ratelimit",
	"too many requests",
	"too_many_requests",
	"requests per minute",
	"requests per second",
	"tokens per minute",
	"tpm",
	"rpm",
	"retry after",
	"retry-after",
	"throttled",
	"throttling",
}

var QuotaPatterns = []string{
	"quota exceeded",
	"quota_exceeded",
	"billing",
	"insufficient_quota",
	"insufficient quota",
	"credit",
	"out of credits",
	"payment required",
	"usage limit",
	"monthly limit",
}

var BalanceEmptyPatterns = []string{
	"add credits",
	"billing hard limit",
	"insufficient funds",
	"out of credits",
	"credit balance",
	"payment required",
	"billing limit reached",
	"no credits remaining",
}

var AuthPatterns = []string{
	"invalid api key",
	"invalid_api_key",
	"unauthorized",
	"authentication",
	"auth failed",
	"invalid token",
	"expired token",
	"permission denied",
	"access denied",
	"invalid bearer",
}

var ModelNotFoundPatterns = []string{
	"model not found",
	"model_not_found",
	"no such model",
	"unknown model",
	"invalid model",
	"model does not exist",
	"model is not available",
	"not supported",
}

var ContentFilterPatterns = []string{
	"content filter",
	"content_filter",
	"safety",
	"blocked",
	"harmful",
	"inappropriate",
	"policy violation",
	"content policy",
	"moderation",
}

var TimeoutPatterns = []string{
	"timeout",
	"timed out",
	"deadline exceeded",
	"connection reset",
	"connection refused",
	"broken pipe",
	"eof",
	"unexpected eof",
}

var NetworkPatterns = []string{
	"network error",
	"network_error",
	"connection error",
	"connection_error",
	"dns",
	"host unreachable",
	"no route to host",
	"socket",
	"tls",
	"ssl",
	"certificate",
}

var ServerErrorPatterns = []string{
	"internal server error",
	"internal_server_error",
	"service unavailable",
	"service_unavailable",
	"bad gateway",
	"bad_gateway",
	"gateway timeout",
	"overloaded",
	"capacity",
	"internal error",
}
