import { exists } from 'libs/utils/exists'
import { View } from 'libs/mvc/view'

/*
 * View representing the help page
 * @class
 * @extends {View}
 */
export class HelpView extends View {
	constructor(serviceState) {
		var el = document.createElement('p2b-help');
    el.serviceState = serviceState;
		super(el);
	}
}