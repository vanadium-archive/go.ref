/*
 * Redirects a stream to another veyron name. It prompts the user to pick
 * a Veyron name before redirecting and allows the user to chose between
 * redirecting all the data or just new incoming data.
 * @fileoverview
 */
import { Logger } from 'libs/logs/logger'
import { register, trigger } from 'libs/mvc/actions'

import { page } from 'runtime/context'

import { RedirectPipeDialogView } from 'views/redirect-pipe-dialog/view'
import { pipe } from 'services/pipe-to-browser-client'
import { getAll as getAllPublishedP2BNames } from 'services/pipe-to-browser-namespace'

var log = new Logger('actions/redirect-pipe');
const ACTION_NAME = 'redirect-pipe';

/*
 * Registers the redirect pipe action
 */
export function registerRedirectPipeAction() {
  register(ACTION_NAME, actionHandler);
}

/*
 * Triggers the redirect pipe action
 * @param {stream} stream Stream object to redirect
 * @param {string} currentPluginName name of the current plugin
 */
export function redirectPipe(stream, currentPluginName) {
  return trigger(ACTION_NAME, stream, currentPluginName);
}

/*
 * Handles the redirect pipe action.
 *
 * @private
 */
function actionHandler(stream, currentPluginName) {
  log.debug('redirect pipe action triggered');

  // display a dialog asking user where to redirect and whether to redirect
  // all the data or just new data.
  var dialog = new RedirectPipeDialogView();
  dialog.open();

  // if user decides to redirect, copy the stream and pipe it.
  dialog.onRedirectAction((name, newDataOnly) => {
    var copyStream = stream.copier.copy(newDataOnly);

    pipe(name, copyStream).then(() => {
      page.showToast('Redirected successfully to ' + name);
    }).catch((e) => {
      page.showToast('FAILED to redirect to ' + name + '. Please see console for error details.');
      log.debug('FAILED to redirect to', name, e);
    });
  });

  // also get the list of all existing P2B names in the namespace and supply it to the dialog
  getAllPublishedP2BNames().then((allNames) => {
    // append current plugin name to the veyron names for better UX
    dialog.existingNames = allNames.map((n) => {
      return n + '/pipe/' + (currentPluginName || ''); //TODO(aghassemi) publish issue
    });
  }).catch((e) => {
    log.debug('getAllPublishedP2BNames failed', e);
  });
}