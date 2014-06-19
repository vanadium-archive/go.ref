import { get as getPipeViewer } from 'pipe-viewers/manager'

/*
 * Allows a pipe-viewer plugin to delegate playing of a stream to another
 * pipe-viewer plugin.
 * Useful for cases where a plugin wants to simply do some transforms on the stream
 * and then have it be played by an existing plugin.
 * @param {string} pipeViewerName of the pipe-viewer to redirect to.
 * @param {Veyron.Stream} stream Stream of data to be redirected.
 * @return {Promise<View>} A promise of an View from the target pipe viewer, which can
 * be returned from the play() method of the caller plugin.
 */
export function redirectPlay(pipeViewerName, stream) {
  return getPipeViewer(pipeViewerName).then((pipeViewer) => {
    return pipeViewer.play(stream);
  });
}