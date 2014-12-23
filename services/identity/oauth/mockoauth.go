package oauth

// mockOAuth is a mock OAuthProvider for use in tests.
type mockOAuth struct{}

func NewMockOAuth() OAuthProvider {
	return &mockOAuth{}
}

func (m *mockOAuth) AuthURL(redirectUrl string, state string) string {
	return redirectUrl + "?state=" + state
}

func (m *mockOAuth) ExchangeAuthCodeForEmail(authCode string, url string) (email string, err error) {
	return "testemail@google.com", nil
}
