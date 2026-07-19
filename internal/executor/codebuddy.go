package executor

func codebuddyHeaders(headers map[string]string, provider string) {
	if provider != "codebuddy" {
		return
	}
	headers["User-Agent"] = "CLI/2.63.2 CodeBuddy/2.63.2"
	headers["X-Product"] = "SaaS"
	headers["X-IDE-Type"] = "CLI"
	headers["X-IDE-Name"] = "CLI"
	headers["X-Domain"] = "codebuddy.ai"
	headers["x-requested-with"] = "XMLHttpRequest"
	headers["x-codebuddy-request"] = "1"
}
