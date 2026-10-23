package identity

// HumanVerifier returns the service's Turnstile verifier, so another form
// (the site's lead form, W1-04) checks tokens the same way send OTP does:
// the same provider, the same fail-closed rule when deployed without it.
func (s *Service) HumanVerifier() HumanVerifier { return s.human }
