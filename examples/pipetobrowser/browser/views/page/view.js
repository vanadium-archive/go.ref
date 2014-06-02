import { View } from 'libs/mvc/view'

/*
 * View representing the application page. Includes page level navigation, toolbar
 * and other page chrome. Used as the container for all other views.
 * @class
 * @extends {View}
 */
export class PageView extends View {
	constructor() {
		var el = document.querySelector('p2b-page');
		super(el);
	}

 /*
  * Displays the given view inside the main area of the page
  * @param {View} view View to display.
  */
	setMainView(view) {
		this.element.setMain(view.element);
	}
}