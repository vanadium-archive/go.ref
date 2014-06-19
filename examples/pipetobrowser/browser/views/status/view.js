import { View } from 'libs/mvc/view'
import { formatDuration } from 'libs/utils/time'

/*
 * View representing the state and interaction for publishing the p2b service.
 * @class
 * @extends {View}
 */
export class StatusView extends View {
	constructor(serviceState) {
		var el = document.createElement('p2b-status');
		el.serviceState = serviceState;

    // TODO(aghassemi) ES6 import syntax doesn't seem to work inside Polymer
    // script tag. Test again when compiling server-side with Traceur.
    el.formatDuration = formatDuration;

		super(el);
	}

/*
 * Event representing user's intention to stop the published service
 * @event
 */
  onStopAction(eventHandler) {
    this.element.addEventListener('stop', () => {
      eventHandler();
    });
  }
}