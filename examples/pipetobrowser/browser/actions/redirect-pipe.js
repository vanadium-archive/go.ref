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
 */
export function redirectPipe(stream) {
  return trigger(ACTION_NAME, stream, name);
}

/*
 * Handles the redirect pipe action.
 *
 * @private
 */
function actionHandler(stream) {
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
}