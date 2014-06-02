import { Logger } from 'libs/logs/logger'
import { register, trigger } from 'libs/mvc/actions'

import { PageView } from 'views/page/view'
import { ErrorView } from 'views/error/view'

var log = new Logger('actions/error');
const ACTION_NAME = 'error';

/*
 * Registers the error action
 */
export function registerErrorAction() {
  register(ACTION_NAME, actionHandler);
}

/*
 * Triggers the error action
 */
export function triggerErrorAction(err) {
  return trigger(ACTION_NAME, err);
}

/*
 * Handles the error action.
 *
 * @private
 */
function actionHandler(err) {
  log.debug('error action triggered');

  // Create an error view
  var errorView = new ErrorView(err);

  // create a page and display the error view in main content area
  var pageView = new PageView();
  pageView.setMainView(errorView);
}

