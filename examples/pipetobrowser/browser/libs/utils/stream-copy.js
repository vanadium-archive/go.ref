var Transform = require('stream').Transform;
var PassThrough = require('stream').PassThrough;
var Buffer = require('buffer').Buffer;
/*
 * A through transform stream keep a copy of the data piped to it and provides
 * functions to create new copies of the stream on-demand
 * @class
 */
export class StreamCopy extends Transform {
  constructor() {
    super();
    this._writableState.objectMode = true;
    this._readableState.objectMode = true;
    // TODO(aghassemi) make this a FIFO buffer with reasonable max-size
    this.buffer = [];
    this.copies = [];
  }

  _transform(chunk, encoding, cb) {
    this.buffer.push(chunk);
    this.push(chunk);
    for (var i=0; i < this.copies.length; i++) {
      this.copies[i].push(chunk);
    }
    cb();
  }

 /*
  * Create a new copy of the stream
  * @param {bool} onlyNewData Whether the copy should include
  * existing data from the stream or just new data.
  * @return {Stream} Copy of the stream
  */
  copy(onlyNewData) {
    var copy = new PassThrough( { objectMode: true });
    if (!onlyNewData) {
      // copy existing data first in the order received
      for (var i = 0; i < this.buffer.length; i++) {
        copy.push(this.buffer[i]);
      }
    }
    this.copies.push(copy);
    return copy;
  }
}
