/*
 * Implements a veyron client that can talk to a P2B service.
 * @fileoverview
 */
import { Logger } from 'libs/logs/logger'
import { config } from 'config/config'

var log = new Logger('services/p2b-client');
var veyron = new Veyron(config.veyron);

/*
 * Pipes a stream of data to the P2B service identified
 * by the given veyron name.
 * @param {string} name Veyron name of the destination service
 * @param {Stream} Stream of data to pipe to it.
 * @return {Promise} Promise indicating if piping was successful or not
 */
export function pipe(name, stream) {
  var client = veyron.newClient();
  return client.bindTo(name).then((remote) => {
    var remoteStream = remote.pipe().stream;
    stream.pipe(remoteStream);
    return Promise.resolve();
  });
}