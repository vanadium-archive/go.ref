package oauth

const (
	MockEmail  = "testemail@google.com"
	MockClient = "test-client"
)

// mockOAuth is a mock OAuthProvider for use in tests.
type mockOAuth struct{}

func NewMockOAuth() OAuthProvider {
	return &mockOAuth{}
}

func (m *mockOAuth) AuthURL(redirectUrl string, state string) string {
	return redirectUrl + "?state=" + state
}

func (m *mockOAuth) ExchangeAuthCodeForEmail(string, string) (string, error) {
	return MockEmail, nil
}

func (m *mockOAuth) GetEmailAndClientName(string, []AccessTokenClient) (string, string, error) {
	return MockEmail, MockClient, nil
}
