// Package flags provides flag definitions for commonly used flags and
// and, where appropriate, implementations of the flag.Value interface
// for those flags to ensure that only valid values of those flags are
// supplied. In general, this package will be used by veyron profiles and
// the runtime implementations, but can also be used by any application
// that wants access to the flags and environment variables it supports.
//
// TODO(cnicolaou): move reading of environment variables to here also,
// flags will override the environment variable settings.
//
// TODO(cnicolaou): implement simply subsetting of the flags to be defined
// and parsed - e.g. for server configs, basic runtime configs/security etc.
package flags
