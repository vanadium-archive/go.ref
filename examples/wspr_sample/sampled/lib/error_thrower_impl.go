package lib

import (
	"errors"

	"veyron2/ipc"
	"veyron2/verror"

	sample "veyron/examples/wspr_sample"
)

// NewCached returns a new implementation of the ErrorThrowerService.
func NewErrorThrower() sample.ErrorThrowerService {
	return &errorThrowerImpl{}
}

type errorThrowerImpl struct{}

func (e *errorThrowerImpl) ThrowAborted(_ ipc.ServerContext) error {
	return verror.Abortedf("Aborted!")
}

func (e *errorThrowerImpl) ThrowBadArg(_ ipc.ServerContext) error {
	return verror.BadArgf("BadArg!")
}

func (e *errorThrowerImpl) ThrowBadProtocol(_ ipc.ServerContext) error {
	return verror.BadProtocolf("BadProtocol!")
}

func (e *errorThrowerImpl) ThrowInternal(_ ipc.ServerContext) error {
	return verror.Internalf("Internal!")
}

func (e *errorThrowerImpl) ThrowNotAuthorized(_ ipc.ServerContext) error {
	return verror.NotAuthorizedf("NotAuthorized!")
}

func (e *errorThrowerImpl) ThrowNotFound(_ ipc.ServerContext) error {
	return verror.NotFoundf("NotFound!")
}

func (e *errorThrowerImpl) ThrowUnknown(_ ipc.ServerContext) error {
	return verror.Unknownf("Unknown!")
}

func (e *errorThrowerImpl) ThrowGoError(_ ipc.ServerContext) error {
	return errors.New("GoError!")
}

func (e *errorThrowerImpl) ThrowCustomStandardError(_ ipc.ServerContext) error {
	return verror.Standard{
		ID:  "MyCustomError",
		Msg: "CustomStandard!",
	}
}

func (e *errorThrowerImpl) ListAllBuiltInErrorIDs(_ ipc.ServerContext) ([]string, error) {
	// TODO(aghassemi) Use when we have enum for error IDs in IDL
	// This is not used yet but the idea is to pass all error types in veyron2/verror to
	// JavaScript so if a new one is added, this test would break and we add the new one to
	// JavaScript as well. There is no way to enumerate all error IDs right now since they
	// are constants and not an Enum. Enum support is coming later.
	return nil, nil
}
