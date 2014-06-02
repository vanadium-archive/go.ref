/*
 * Pipe viewer manager is used to load and get an instance of a pipe viewer
 * given its name.
 *
 * Manager handles on-demand loading and caching of pipe viewers.
 * @fileoverview
 */

import { isAbsoulteUrl } from 'libs/utils/url'
import { Logger } from 'libs/logs/logger'

var log = new Logger('pipe-viwer/manager');

// cache loaded viewers
var loadedPipeViewers = {};

/*
 * Asynchronously loads and returns a PipeViewer plugin instance given its name.
 * @param {string} name Unique name of the viewer.
 * @return {Promise<PipeViewer>} pipe viewer for the given name
 */
export function get(name) {
  if(isLoaded(name)) {
    return Promise.resolve(new loadedPipeViewers[name]());
  }

  return loadViewer(name).then((viewerClass) => {
    return new viewerClass();
  }).catch((e) => { return Promise.reject(e); });
}

/*
 * Tests whether the viewer plugin is already loaded or not.
 * @param {string} name Unique name of the viewer.
 * @return {string} Whether the viewer plugin is already loaded or not.
 *
 * @private
 */
function isLoaded(name) {
  return loadedPipeViewers[name] !== undefined
}

/*
 * Registers a pipeViewer under a unique name and make it available to be called
 * @param {string} name Unique name of the viewer.
 * @return {Promise} when import completes.
 *
 * @private
 */
function loadViewer(name) {
  log.debug('loading viewer:', name);

  var path = getPath(name);
  return System.import(path).then((module) => {
    var pipeViewerClass = module.default;
    loadedPipeViewers[name] = pipeViewerClass;
    return pipeViewerClass;
  }).catch((e) => {
    log.debug('could not load viewer JavaScript module for:', name, e);
    return Promise.reject(e);
  })
}

/*
 * Returns the path to a pipe viewer module location based on its name.
 * @param {string} name Unique name of the viewer.
 * @return {string} path to a pipe viewer module location.
 *
 * @private
 */
function getPath(name) {
  if(isAbsoulteUrl(name)) {
    return name;
  } else {
    return 'pipe-viewers/builtin/' + name + '/' + name;
  }
}
