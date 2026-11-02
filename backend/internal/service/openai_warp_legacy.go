package service

// openAIWarpOrProxyURL preserves the account's selected exit while the
// administrator has disabled mandatory WARP routing.
func openAIWarpOrProxyURL(proxyURL string) string {
	if OpenAIWarpEnabled() {
		return OpenAIWarpProxyURL()
	}
	return proxyURL
}
