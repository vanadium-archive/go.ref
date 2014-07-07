/*
 * App configuration
 * @fileoverview
 */

var veyronLogLevels = Veyron.logLevels;

export var config = {
  veyron: {
    identityServer: 'http://localhost:5163/random/',
    proxy: 'http://localhost:5165',
    logLevel: veyronLogLevels.INFO
  },
  namespaceRoot: '/localhost:5167',
  publishNamePrefix: 'google/p2b'
}