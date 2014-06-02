import { Logger } from 'libs/logs/logger'
import { register, trigger } from 'libs/mvc/actions'

import { get as getPipeViewer } from 'pipe-viewers/manager'

import { triggerErrorAction } from 'actions/error'

import { PageView } from 'views/page/view'
import { PipeViewerPlaceholderView } from 'views/pipe-viewer-placeholder/view'

var log = new Logger('actions/display-pipe-viewer');
const ACTION_NAME = 'displayPipeViewer';

/*
 * Registers the display pipe viewer action
 */
export function registerDisplayPipeViewerAction() {
  register(ACTION_NAME, actionHandler);
}

/*
 * Triggers the display pipe viewer action
 */
export function triggerDisplayPipeViewerAction(name, stream) {
  return trigger(ACTION_NAME, name, stream);
}

/*
 * Handles the displayPipeViewer action.
 * @param {string} name Name of the Pipe Viewer that is requested to play the stream.
 * @param {Veyron.Stream} stream Stream of bytes from the p2b client.
 *
 * @private
 */
function actionHandler(name, stream) {
  log.debug('displayPipeViewer action triggered');

  var viewerPlaceholderView = new PipeViewerPlaceholderView();

  // Get the plugin that can render the stream, ask it to play it and display
  // the element returned by the pipeViewer.
  getPipeViewer(name).then((pipeViewer) => {
    return pipeViewer.play(stream);
  }).then((viewerView) => {
    viewerPlaceholderView.showViewer(viewerView);
  }).catch((e) => { triggerErrorAction(e); });

  // create a page and display the viewer placeholder view in main content area
  var pageView = new PageView();
  pageView.setMainView(viewerPlaceholderView);
}
