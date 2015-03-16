// Package consts defines named constants whose values are interpreted by the flags package.
package consts

const (
	// Environment variable whose value points to a directory containing
	// the state of a Principal.  (Private key, blessings, recognized root
	// certificates etc.)
	VeyronCredentials = "VEYRON_CREDENTIALS"
	// Prefix of all environment variables that point to roots of the
	// vanadium namespace, used to resolve non-rooted object names.
	NamespaceRootPrefix = "NAMESPACE_ROOT"
	// Environment variable containing a comma-separated list of i18n
	// catalogue files to be loaded at startup.
	I18nCatalogueFiles = "VANADIUM_I18N_CATALOGUE"
)
