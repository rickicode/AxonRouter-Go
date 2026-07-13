package proxypool

import "testing"

func TestTestProxyInvalidURL(t *testing.T) {
	res := TestProxy("not-a-valid-url", "http", "")
	if res.OK {
		t.Errorf("TestProxy returned OK for an invalid URL")
	}
}
