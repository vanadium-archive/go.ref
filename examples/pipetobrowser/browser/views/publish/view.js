import { View } from 'libs/mvc/view'

/*
 * View representing the state and interaction for publishing the p2b service.
 * @class
 * @extends {View}
 */
export class PublishView extends View {
  constructor(publishState) {
    var el = document.createElement('p2b-publish');
    el.publishState = publishState;
    super(el);
  }

/*
 * Event representing user's intention to publish the p2b service under the provided name
 * @event
 * @type {string} Requested name for service to be published under
 */
  onPublishAction(eventHandler) {
    this.element.addEventListener('publish', (e) => {
      eventHandler(e.detail.publishName);
    });
  }
}