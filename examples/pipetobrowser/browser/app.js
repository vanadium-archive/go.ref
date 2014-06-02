import { Logger } from 'libs/logs/logger'

import { registerHomeAction,  triggerHomeAction } from 'actions/home'
import { registerErrorAction } from 'actions/error'
import { registerDisplayPipeViewerAction } from 'actions/display-pipe-viewer'

var log = new Logger('app');

export function start() {
  log.debug('start called');

  // Register the action handlers for the application
  registerActions();

  // Start by triggering the home action
  triggerHomeAction();
}

/*
 * Registers the action handlers for the application.
 * Actions are cohesive pieces of functionality that can be triggered from
 * any other action in a decoupled way by just using the action name.
 *
 * @private
 */
function registerActions() {
  log.debug('registering actions');

  registerHomeAction();
  registerErrorAction();
  registerDisplayPipeViewerAction();
}