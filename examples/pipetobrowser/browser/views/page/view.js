import { View } from 'libs/mvc/view'

/*
 * View representing the application page. Includes page level navigation, toolbar
 * and other page chrome. Used as the container for all other views.
 * @class
 * @extends {View}
 */
export class PageView extends View {
	constructor() {
		var el = document.createElement('p2b-page');
    el.subPages = [];
		super(el);
	}

 /*
  * Displays the given view inside the sub page area identified by the key.
  * @param {String} subPageKey Key for the sub page to display.
  * @param {View} view View to display.
  */
	setSubPageView(subPageKey, view) {
		this.element.setSubPage(subPageKey, view.element);
	}

 /*
  * Displayed a message toast for a few seconds e.g. "Saved Successfully"
  * @type {SubPageItem}
  */
  showToast(text) {
    this.element.showToast(text);
  }

 /*
  * Collection of sub pages
  * @type {SubPageItem}
  */
  get subPages() {
    return this.element.subPages;
  }

 /*
  * Title of the page
  * @type {string}
  */
  set title(title) {
    this.element.pageTitle = title;
  }

}

/*
 * SubPageItem represents top level sub pages that have a sidebar navigation link
 * and a content area which gets displayed when corresponding sidebar item
 * is activated by the end user.
 * @param {String} key Unique identified for this sub page
 * @class
 */
export class SubPageItem {
  constructor(key) {
    /*
     * Name of the page. Normally displayed as the sidebar navigation item text
     * @type {String}
     */
    this.name = name;

    /*
     * Unique identified for this sub page
     * @type {String}
     */
    this.key = key;

    /*
     * Function that's called when user activates the sub page, normally by clicking
     * the sidebar navigation items for the sub page
     * @type {Function}
     */
    this.onActivate = null;
  }
}