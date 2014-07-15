/*
 * Pipe viewer manager is used to load and get an instance of a pipe viewer
 * given its name.
 *
 * Manager handles on-demand loading and caching of pipe viewers.
 * @fileoverview
 */

import { isAbsoulteUrl } from 'libs/utils/url'
import { Logger } from 'libs/logs/logger'

/*
 * Preload certain common builtin plugins.
 * Plugins are normally loaded on demand and this makes the initial bundle larger
 * but common plugins should be preloaded for better performance.
 * This is kind of a hack as it simply exposes a path to these
 * plugins so that build bundler finds them and bundles them with the reset of the app
 */
import { default as plugin } from './builtin/vlog/plugin'
import { default as plugin } from './builtin/image/plugin'
import { default as plugin } from './builtin/console/plugin'

var log = new Logger('pipe-viewer/manager');

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
    var errMessage = 'could not load viewer for: ' + name;
    log.debug(errMessage, e);
    return Promise.reject(new Error(errMessage));
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
    var encodedName = encodeURIComponent(name);
    System.paths[encodedName] = name;
    return encodedName;
  } else {
    return 'pipe-viewers/builtin/' + name + '/plugin';
  }
}
