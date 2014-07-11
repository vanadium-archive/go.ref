/*
 * Navigates to neighborhood page displaying list of P2B names that are online
 * @fileoverview
 */

import { Logger } from 'libs/logs/logger'
import { register, trigger } from 'libs/mvc/actions'

import { displayError } from 'actions/display-error'
import { page } from 'runtime/context'

import { NeighborhoodView } from 'views/neighborhood/view'
import { getAll as getAllPublishedP2BNames } from 'services/pipe-to-browser-namespace'

var log = new Logger('actions/navigate-neighborhood');
var ACTION_NAME = 'neighborhood';

/*
 * Registers the action
 */
export function registerNavigateNeigbourhoodAction() {
  register(ACTION_NAME, actionHandler);
}

/*
 * Triggers the action
 */
export function navigateNeigbourhood() {
  return trigger(ACTION_NAME);
}

/*
 * Handles the action.
 *
 * @private
 */
function actionHandler() {
  log.debug('navigate neighborhood triggered');

  // create an neighborhood view
  var neighborhoodView = new NeighborhoodView();

  // get all the online names and set it on the view
  getAllPublishedP2BNames().then((allNames) => {
    neighborhoodView.existingNames = allNames;
  }).catch((e) => { displayError(e); });

  page.setSubPageView('neighborhood', neighborhoodView);
}