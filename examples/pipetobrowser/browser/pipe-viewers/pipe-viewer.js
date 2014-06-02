/*
 * Defines the interface expected to be implemented by any pipe viewer plugin.
 * A PipeViewer is an object that can render a stream of data.
 *
 * It has a unique name which is used to find the viewer plugin and direct
 * requests to it.
 *
 * It has a play function that will be called and supplied by a stream object.
 * play(stream) needs to return a View or promise of a View
 * that will be appended to the UI by the p2b framework.
 * If a promise, p2b UI will display a loading widget until promise is resolved.
 */
export class PipeViewer {

  /*
   * @property {string} name Unique name of the viewer.
   */
  get name() {
    throw new Error('Abstract method. Must be implemented by subclasses');
  }

  /*
   * play() function is called by the p2b framework when a pipe request for the
   * this specific pipe viewer comes in.
   * @param {Veyron.Stream} stream Stream of data to be displayed.
   * @return {Promise<View>|{View}} a View or a promise of an
   * View that p2b can display.
   */
  play(stream) {
    throw new Error('Abstract method. Must be implemented by subclasses');
  }
}
