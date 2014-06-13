/*
 * Error action displays the error page displaying the given error
 * @fileoverview
 */

import { Logger } from 'libs/logs/logger'
import { register, trigger } from 'libs/mvc/actions'

import { ErrorView } from 'views/error/view'

import { page } from 'runtime/context'

var log = new Logger('actions/display-error');
const ACTION_NAME = 'error';

/*
 * Registers the error action
 */
export function registerDisplayErrorAction() {
  register(ACTION_NAME, actionHandler);
}

/*
 * Triggers the error action
 */
export function displayError(err) {
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

  // Display the error view in Home sub-page area
  page.setSubPageView('home', errorView);
}