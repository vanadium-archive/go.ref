/*
 * Home action displays the Home page. Home could be status or publish view
 * depending on the state of the P2B service.
 * @fileoverview
 */

import { Logger } from 'libs/logs/logger'
import { register, trigger } from 'libs/mvc/actions'

import { publish, stopPublishing, state as publishState } from 'services/pipe-to-browser-server'

import { displayError } from 'actions/display-error'
import { addPipeViewer } from 'actions/add-pipe-viewer'

import { PublishView } from 'views/publish/view'
import { StatusView } from 'views/status/view'

import { page } from 'runtime/context'

var log = new Logger('actions/navigate-home-page');
const ACTION_NAME = 'home';

/*
 * Registers the home action
 */
export function registerNavigateHomePageAction() {
  register(ACTION_NAME, actionHandler);
}

/*
 * Triggers the home action
 */
export function navigateHomePage() {
  return trigger(ACTION_NAME);
}

/*
 * Handles the home action.
 *
 * @private
 */
function actionHandler() {
  log.debug('home action triggered');

  var mainView;

  // Show status view if already published, otherwise show publish view
  if (publishState.published) {
    showStatusView();
  } else {
    showPublishView();
  }
}

/*
 * Displays the Status view
 *
 * @private
 */
function showStatusView() {
  // Create a status view  and bind (dynamic) publish state with the view
  var statusView = new StatusView(publishState);

  // Stop when user tells us to stop the service
  statusView.onStopAction(() => {
    stopPublishing().then(function() {
      navigateHomePage();
    }).catch((e) => { displayError(e); });
  });

  // Display the status view in main content area and select the sidebar item
  page.title = 'Status';
  page.setSubPageView('home', statusView);
}

/*
 * Displays the Publish view
 *
 * @private
 */
function showPublishView() {
  // Create a publish view
  var publishView = new PublishView();

  // Publish p2b when user tells us to do so and then show status page.
  publishView.onPublishAction((publishName) => {
    publish(publishName, pipeRequestHandler).then(function() {
      showStatusView();
    }).catch((e) => { displayError(e); });
  });

  // Display the publish view in main content area and select the sidebar item
  page.title = 'Publish';
  page.setSubPageView('home', publishView);
}

/*
 * pipeRequestHandler is called by the p2b service whenever a new request comes in.
 * We simply delegate to the addPipeViewer action.
 * @param {string} name Name of the Pipe Viewer that is requested to play the stream.
 * @param {Veyron.Stream} stream Stream of bytes from the p2b client.
 *
 * @private
 */
function pipeRequestHandler(name, stream) {
  return addPipeViewer(name, stream);
}