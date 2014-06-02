import { View } from 'libs/mvc/view'

/*
 * View representing a placeholder that will be filled with a pipe viewer element.
 * Acts as the parent of pipe viewers and handles loading UI until actual viewer is ready.
 * @class
 * @extends {View}
 */
export class PipeViewerPlaceholderView extends View {
  constructor() {
    var el = document.createElement('p2b-pipe-viewer-placeholder');
    super(el);
  }

 /*
  * Displays the given view inside the placeholder
  * @param {View} view View to display.
  */
  showViewer(view) {
    this.element.showViewer(view.element);
  }
}