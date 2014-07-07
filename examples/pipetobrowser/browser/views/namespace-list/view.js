import { View } from 'libs/mvc/view'

/*
 * View showing a list of all p2b services from the namespace
 * @class
 * @extends {View}
 */
export class NamespaceListView extends View {
	constructor(items) {
		var el = document.createElement('p2b-namespace-list');
		el.items = items;
		super(el);
	}

/*
 * Event that fires when user selects an item from the list.
 * @event
 * @type {string} name of the item that was selected
 */
  onSelectAction(eventHandler) {
    this.element.addEventListener('select', (e) => {
      eventHandler(e.detail);
    });
  }
}