package oauth

// OAuthProvider authenticates users to the identity server via the OAuth2 Web Server flow.
type OAuthProvider interface {
	// AuthURL is the URL the user must visit in order to authenticate with the OAuthProvider.
	// After authentication, the user will be re-directed to redirectURL with the provided state.
	AuthURL(redirectUrl string, state string) (url string)
	// ExchangeAuthCodeForEmail exchanges the provided authCode for the email of an
	// authenticated user.
	ExchangeAuthCodeForEmail(authCode string, url string) (email string, err error)
}
