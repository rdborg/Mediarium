package api

// TestSetupCode is this run's first-run setup code, for the setup tests.
func (s *Server) TestSetupCode() string { return s.setupCode() }
