import { View } from 'libs/mvc/view'

/*
 * View displaying a list of currently published PipeToBrowsers instances
 * @class
 * @extends {View}
 */
export class NeighborhoodView extends View {
	constructor() {
		var el = document.createElement('p2b-neighborhood');
		super(el);
	}

 /*
  * List of existing names to show
  * @type {Array<string>}
  */
  set existingNames(val)  {
    this.element.existingNames = val;
  }
}