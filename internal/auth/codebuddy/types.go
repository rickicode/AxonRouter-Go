package codebuddy

import "time"

const (
	stateURL        = "https://codebuddy.ai/v2/plugin/auth/state"
	tokenURL        = "https://codebuddy.ai/v2/plugin/auth/token"
	refreshURL      = "https://codebuddy.ai/v2/plugin/auth/token/refresh"
	userAgent       = "CLI/2.63.2 CodeBuddy/2.63.2"
	platform        = "CLI"
	pollInterval    = 5 * time.Second
	maxPollDuration = 5 * time.Minute

	httpClientTimeout = 30 * time.Second
)

type deviceFlowState struct {
	state   string
	authUrl string
}

type deviceCodeResponse struct {
	state   string
	authUrl string
}

type stateResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		State   string `json:"state"`
		AuthUrl string `json:"authUrl"`
	} `json:"data"`
}

type tokenResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		TokenType    string `json:"tokenType"`
		ExpiresIn    int64  `json:"expiresIn"`
	} `json:"data"`
}
