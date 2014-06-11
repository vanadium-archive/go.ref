/*
 * AddPipeViewer action can be used to add a new viewer to the pipes view
 * this action can be run at anytime and user can be on any view and this action
 * will still work.
 * Depending on user preferences, user might be presented with a confirmation
 * dialog to accept seeing the incoming pipe.
 * @fileoverview
 */

import { Logger } from 'libs/logs/logger'
import { register, trigger } from 'libs/mvc/actions'

import { get as getPipeViewer } from 'pipe-viewers/manager'

import { displayError } from 'actions/display-error'
import { navigatePipesPage } from 'actions/navigate-pipes-page'

import { LoadingView } from 'views/loading/view'

import { pipesViewInstance } from 'runtime/context'

var log = new Logger('actions/add-pipe-viewer');
const ACTION_NAME = 'addPipeViewer';
var pipesPerNameCounter = {};

/*
 * Registers the add pipe viewer action
 */
export function registerAddPipeViewerAction() {
  register(ACTION_NAME, actionHandler);
}

/*
 * Triggers the add pipe viewer action
 */
export function addPipeViewer(name, stream) {
  return trigger(ACTION_NAME, name, stream);
}

/*
 * Handles the addPipeViewer action.
 * @param {string} name Name of the Pipe Viewer that is requested to play the stream.
 * @param {Veyron.Stream} stream Stream of bytes from the p2b client.
 *
 * @private
 */
function actionHandler(name, stream) {
  log.debug('addPipeViewer action triggered');

  // Book keeping of number of pipe-viewers per name, we use this to generate
  // display names and keys like image #3
  var count = (pipesPerNameCounter[name] || 0) + 1;
  pipesPerNameCounter[name] = count;
  var tabKey = name + count;
  var tabName = name + ' #' + count;

  // Get the plugin that can render the stream, ask it to play it and display
  // the element returned by the pipeViewer.
  getPipeViewer(name).then((pipeViewer) => {
    return pipeViewer.play(stream);
  }).then((pipeViewerView) => {
    // replace the loading view with the actual viewerView
    pipesViewInstance.replaceTabView(tabKey, pipeViewerView);
  }).catch((e) => { displayError(e); });

  // Add a new tab and show a loading indicator for now,
  // then replace the loading view with the actual viewer when ready
  var loadingView = new LoadingView();
  pipesViewInstance.addTab(tabKey, tabName, loadingView);

  // Take the user to the pipes view.
  navigatePipesPage();
}