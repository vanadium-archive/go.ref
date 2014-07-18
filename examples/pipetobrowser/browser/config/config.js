/*
 * App configuration
 * @fileoverview
 */
var veyronLogLevels = Veyron.logLevels;

export var config = {
  veyron: {
    proxy: 'http://localhost:7776',
    logLevel: veyronLogLevels.INFO
  },
  namespaceRoot: '/proxy.envyor.com:8101',
  publishNamePrefix: 'google/p2b'
}