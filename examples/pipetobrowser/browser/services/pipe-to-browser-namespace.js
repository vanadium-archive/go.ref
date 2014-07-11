/*
 * Implements a veyron client that talks to the namespace service and finds all
 * the P2B services that are available.
 * @fileoverview
 */
import { Logger } from 'libs/logs/logger'
import { config } from 'config/config'

var log = new Logger('services/p2b-namespace');
var veyron = new Veyron(config.veyron);
var client = veyron.newClient();

/*
 * Finds all the P2B services that are published by querying the namespace.
 * @return {Promise} Promise resolving to an array of names for all published
 * P2B services
 */
export function getAll() {
  return client.bindTo(config.namespaceRoot).then((namespace) => {
    var globResult = namespace.glob('google/p2b/*');
    var p2bServices = [];
    globResult.stream.on('data', (p2bServiceName) => {
      p2bServices.push(p2bServiceName.name);
    });

    // wait until all the data arrives then return the collection
    return globResult.then(() => {
      return p2bServices;
    });
  });
}