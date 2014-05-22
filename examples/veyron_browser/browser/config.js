/*
 * Optional configuration to be used to create the Veyron object.
 * It specifies location of services that Veyron depends on such as identity
 * server and proxy daemons.
 *
 * If not specified, public Google-hosted daemons will be used.
 * TODO(aghassemi) Use Vonery and remove this before release
 */
var veyronConfig = {
  // Log severity, INFO means log anything equal or more severe than INFO
  // One of NOLOG, ERROR, WARNING, DEBUG, INFO
  'logLevel': Veyron.logLevels.INFO,

  // Server to use for identity
  'identityServer': 'http://localhost:5163/random/',

  // Daemon that handles JavaScript communication with the rest of Veyron
  'proxy': 'http://localhost:5165'
};
