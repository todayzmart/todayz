package identity

// OTPSender returns the SMS sender of the sign-in code, so the field owner
// code (W3-06, A-04) goes out the same way: the same provider, the same
// local log-only sender.
//
// TODO(lead): the owner code reuses the sign-in DLT template text; a
// dedicated "confirm your shop on TodayZ" template needs registering first.
func (s *Service) OTPSender() SMSSender { return s.sms }

// AllowsTestCode reports whether 000000 is accepted for the reserved test
// numbers (local and test only, on a loopback API), so the owner code
// follows the same rule as sign-in.
func (s *Service) AllowsTestCode() bool { return s.allowTestCode }
