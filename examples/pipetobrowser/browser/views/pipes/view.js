import { View } from 'libs/mvc/view'

/*
 * View representing a collection of pipes displayed in tabs.
 * this view manages the tabs and the empty message when no pipes available
 * @class
 * @extends {View}
 */
export class PipesView extends View {
  constructor() {
    var el = document.createElement('p2b-pipes');
    super(el);
  }

 /*
  * Adds the given view as a new pipe viewer tab
  * @param {string} key A string key identifier for the tab.
  * @param {string} name A short name for the tab that will be displayed as
  * the tab title
  * @param {View} view View to show inside the tab.
  */
  addTab(key, name, view) {
    this.element.addTab(key, name, view.element);
  }

 /*
  * Replaces the content of the tab identified via key by the new view.
  * @param {string} key A string key identifier for the tab.
  * @param {View} view View to replace the current tab content
  */
  replaceTabView(key, newView) {
    this.element.replaceTabContent(key, newView.element);
  }
}