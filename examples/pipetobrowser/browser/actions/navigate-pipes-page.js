/*
 * Pipes action displays the Pipes page. It is normally triggered by clicking
 * the Pipes navigation item in the side bar
 * @fileoverview
 */

import { Logger } from 'libs/logs/logger'
import { register, trigger } from 'libs/mvc/actions'

import { page, pipesViewInstance } from 'runtime/context'

var log = new Logger('actions/navigate-pipes-page');
var ACTION_NAME = 'pipes';

/*
 * Registers the pipes action
 */
export function registerNavigatePipesPageAction() {
  register(ACTION_NAME, actionHandler);
}

/*
 * Triggers the pipes action
 */
export function navigatePipesPage(err) {
  return trigger(ACTION_NAME, err);
}

/*
 * Handles the pipes action.
 *
 * @private
 */
function actionHandler() {
  log.debug('pipes action triggered');

  // display the singleton pipesViewInstance main content area
  page.title = 'Pipes';
  page.setSubPageView('pipes', pipesViewInstance);
}