import { View } from 'libs/mvc/view'

/*
 * View representing a loading indicator
 * @class
 * @extends {View}
 */
export class LoadingView extends View {
  constructor() {
    var el = document.createElement('p2b-loading');
    super(el);
  }
}