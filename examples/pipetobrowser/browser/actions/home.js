import { Logger } from 'libs/logs/logger'
import { register, trigger } from 'libs/mvc/actions'

import { publish, state as publishState } from 'services/pipe-to-browser'

import { triggerErrorAction } from 'actions/error'
import { triggerDisplayPipeViewerAction } from 'actions/display-pipe-viewer'

import { PageView } from 'views/page/view'
import { PublishView } from 'views/publish/view'

var log = new Logger('actions/home');
const ACTION_NAME = 'home';

/*
 * Registers the home action
 */
export function registerHomeAction() {
  register(ACTION_NAME, actionHandler);
}

/*
 * Triggers the home action
 */
export function triggerHomeAction() {
  return trigger(ACTION_NAME);
}

/*
 * Handles the home action.
 *
 * @private
 */
function actionHandler() {
  log.debug('home action triggered');

  // Create a publish view and bind (dynamic) publish state (publishing, published, name) with the view
  var publishView = new PublishView(publishState);

  // Publish p2b when user tells us to do so.
  publishView.onPublishAction((publishName) => {
    publish(publishName, pipeRequestHandler).catch((e) => {  triggerErrorAction(e); } );
  });

  // create a page and display the publish view in main content area
  var pageView = new PageView();
  pageView.setMainView(publishView);
}

/*
 * pipeRequestHandler is called the p2b service whenever a new request comes in.
 * We simply delegate to the displayPipeViewer action.
 * @param {string} name Name of the Pipe Viewer that is requested to play the stream.
 * @param {Veyron.Stream} stream Stream of bytes from the p2b client.
 *
 * @private
 */
function pipeRequestHandler(name, stream) {
  return triggerDisplayPipeViewerAction(name, stream);
}