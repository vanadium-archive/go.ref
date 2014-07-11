import { default as Stream } from "nodelibs/stream"

var Transform = Stream.Transform;
/*
 * A through transform stream that counts number of bytes being piped to it
 * @param {function} onUpdate Callback function that gets called with number of
 * bytes read when a chunk is read
 * @class
 */
export class StreamByteCounter extends Transform {
  constructor(onUpdate) {
    super();
    this._onUpdate = onUpdate;
  }

  _transform(chunk, encoding, cb) {
    this._onUpdate(chunk.length);
    this.push(chunk)
    cb();
  }
}
