package identity

// OTPSender returns the SMS sender of the sign-in code.
func (s *Service) OTPSender() SMSSender { return s.sms }

// OwnerCodeSender returns the sender of the field owner code (W3-06, A-04):
// the same provider and local log-only sender as sign-in, with its own DLT
// template (tz_owner_code in SMS_NOTICE_TEMPLATE_IDS) once that is
// registered, else the sign-in template. A sender injected through Deps.SMS
// (tests) is used as it is.
func (s *Service) OwnerCodeSender() SMSSender {
	a, ok := s.sms.(*awsOTP)
	id := s.cfg.Providers.SMSNoticeTemplateIDs[ownerCodeTemplate]
	if !ok || id == "" {
		return s.sms
	}
	return &awsOTP{client: a.client, template: id, text: ownerCodeText}
}

// AllowsTestCode reports whether 000000 is accepted for the reserved test
// numbers (local and test only, on a loopback API), so the owner code
// follows the same rule as sign-in.
func (s *Service) AllowsTestCode() bool { return s.allowTestCode }
