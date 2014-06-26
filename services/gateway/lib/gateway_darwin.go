package gateway

// TODO(mattr): This is a temporary placeholder to get compiles basically
// working on a mac.  Obviously a real implementation is needed.

func New(proximityService string, forceClient bool) (*Service, error) {
	return &Service{}, nil
}

type Service struct {
}

// Stop stops the gateway service.
func (s *Service) Stop() {
}
