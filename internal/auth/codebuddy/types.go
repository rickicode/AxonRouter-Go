package codebuddy

import "time"

const (
	stateURL        = "https://www.codebuddy.ai/v2/plugin/auth/state"
	tokenURL        = "https://www.codebuddy.ai/v2/plugin/auth/token"
	refreshURL      = "https://www.codebuddy.ai/v2/plugin/auth/token/refresh"
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
		Email        string `json:"email"`
		Nickname     string `json:"nickname"`
	} `json:"data"`
}

type accountsResponse struct {
	Code int           `json:"code"`
	Msg  string        `json:"msg"`
	Data accountsData  `json:"data"`
}

type accountsData struct {
	Accounts []codebuddyAccount `json:"accounts"`
}

type codebuddyAccount struct {
	UID            string `json:"uid"`
	Nickname       string `json:"nickname"`
	UIN            string `json:"uin"`
	Type           string `json:"type"`
	LastLogin      bool   `json:"lastLogin"`
	EnterpriseID   string `json:"enterpriseId"`
	EnterpriseName   string `json:"enterpriseName"`
}

type paymentTypeResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		PaymentType string `json:"paymentType"`
	} `json:"data"`
}
