import { View } from 'libs/mvc/view'

/*
 * View representing a dialog that asks the user where they want to redirect
 * the current pipe and whether only new data should be redirected
 * @class
 * @extends {View}
 */
export class RedirectPipeDialogView extends View {
  constructor() {
    var el = document.createElement('p2b-redirect-pipe-dialog');
    super(el);
  }

  /*
   * Opens the Redirect Pipe Dialog
   */
  open() {
    this.element.open();
  }

 /*
  * Event representing user's intention to redirect
  * @event
  * @type {string} Requested name for service to be redirected
  * @type {boolean} Whether only new data should be redirected
  */
  onRedirectAction(eventHandler) {
    this.element.addEventListener('redirect', (e) => {
      eventHandler(e.detail.name, e.detail.newDataOnly);
    });
  }

}